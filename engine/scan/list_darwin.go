package scan

import (
	"encoding/binary"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// macOS: getattrlistbulk(2) returns name, type, size, mtime, inode and link count for a whole
// batch of directory entries per syscall — several times faster than readdir + lstat per entry.

const (
	sysGetattrlistbulk = 461

	attrBitMapCount = 5

	attrCmnName          = 0x00000001
	attrCmnDevID         = 0x00000002
	attrCmnObjType       = 0x00000008
	attrCmnModTime       = 0x00000400
	attrCmnFlags         = 0x00040000
	attrCmnFileID        = 0x02000000
	attrCmnError         = 0x20000000
	attrCmnReturnedAttrs = 0x80000000

	attrFileLinkCount = 0x00000001
	attrFileAllocSize = 0x00000004

	fsoptNoFollow       = 0x00000001
	fsoptPackInvalAttrs = 0x00000008

	vREG = 1
	vDIR = 2
	vLNK = 5
)

type attrList struct {
	bitmapCount uint16
	reserved    uint16
	commonAttr  uint32
	volAttr     uint32
	dirAttr     uint32
	fileAttr    uint32
	forkAttr    uint32
}

var bulkAttrs = attrList{
	bitmapCount: attrBitMapCount,
	commonAttr:  attrCmnReturnedAttrs | attrCmnName | attrCmnDevID | attrCmnObjType | attrCmnModTime | attrCmnFlags | attrCmnFileID | attrCmnError,
	fileAttr:    attrFileLinkCount | attrFileAllocSize,
}

// Record layout (FSOPT_PACK_INVAL_ATTRS keeps the common block fixed; directories stop after fileid):
//
//	0 length u32 | 4 returned attribute_set_t (5×u32) | 24 error u32 | 28 name attrreference (off i32, len u32)
//	36 devid u32 | 40 objtype u32 | 44 modtime timespec (2×i64) | 60 st_flags u32 | 64 fileid u64
//	72 linkcount u32 | 76 allocsize i64   (files only)
const (
	offFileRet = 16
	offErr     = 24
	offName    = 28
	offDev     = 36
	offType    = 40
	offMtime   = 44
	offFlags   = 60
	offIno     = 64
	offNlink   = 72
	offAlloc   = 76
	recMin     = 72 // directories carry no file attributes
	recFile    = 84

	sfDataless = 0x40000000 // iCloud / File Provider placeholder: contents not on this disk
)

var bufPool = sync.Pool{New: func() any { b := make([]byte, 256<<10); return &b }}

func listDir(path string, out *[]rawEntry) error {
	var fd int
	var err error
	// the Go runtime's preemption signal can interrupt open(2): retry, or the directory silently vanishes
	for try := 0; try < 8; try++ {
		if fd, err = syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0); err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	bp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bp)
	buf := *bp
	start := len(*out)
	for {
		n, _, e := syscall.Syscall6(sysGetattrlistbulk, uintptr(fd), uintptr(unsafe.Pointer(&bulkAttrs)),
			uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), fsoptPackInvalAttrs|fsoptNoFollow, 0)
		if e != 0 {
			if e == syscall.EINTR {
				continue
			}
			if (e == syscall.ENOTSUP || e == syscall.ENOSYS || e == syscall.EINVAL) && len(*out) == start {
				return listDirGeneric(path, out) // filesystem without bulk support
			}
			return e
		}
		if n == 0 {
			return nil
		}
		p := 0
		for i := 0; i < int(n); i++ {
			if p+4 > len(buf) {
				break
			}
			rl := int(binary.LittleEndian.Uint32(buf[p:]))
			if rl < recMin || p+rl > len(buf) {
				break
			}
			r := buf[p : p+rl]
			p += rl
			noff := int(int32(binary.LittleEndian.Uint32(r[offName:])))
			nlen := int(binary.LittleEndian.Uint32(r[offName+4:]))
			ns := offName + noff
			if nlen <= 0 || ns+nlen > len(r) {
				continue
			}
			name := string(r[ns : ns+nlen-1]) // trailing NUL
			if binary.LittleEndian.Uint32(r[offErr:]) != 0 {
				*out = append(*out, rawEntry{name: name, err: true})
				continue
			}
			typ := binary.LittleEndian.Uint32(r[offType:])
			e := rawEntry{
				name:  name,
				dir:   typ == vDIR,
				link:  typ == vLNK,
				mtime: uint32(int64(binary.LittleEndian.Uint64(r[offMtime:]))),
				dev:   uint64(binary.LittleEndian.Uint32(r[offDev:])),
				ino:   binary.LittleEndian.Uint64(r[offIno:]),
				cloud: binary.LittleEndian.Uint32(r[offFlags:])&sfDataless != 0,
			}
			if !e.dir && len(r) >= recFile {
				fret := binary.LittleEndian.Uint32(r[offFileRet:])
				if fret&attrFileAllocSize != 0 {
					e.size = int64(binary.LittleEndian.Uint64(r[offAlloc:]))
				}
				if fret&attrFileLinkCount != 0 {
					e.nlink = binary.LittleEndian.Uint32(r[offNlink:])
				}
			}
			*out = append(*out, e)
		}
	}
}

// Never let a scan download iCloud / File Provider content: with materialisation off, reading a
// dataless directory fails fast instead of blocking until fileproviderd fetches it.
func init() {
	const (
		sysIopolicysys                   = 322
		iopolCmdSet                      = 1
		iopolTypeVfsMaterializeDataless  = 3
		iopolScopeProcess                = 0
		iopolMaterializeDatalessFilesOff = 1
	)
	p := [3]int32{iopolScopeProcess, iopolTypeVfsMaterializeDataless, iopolMaterializeDatalessFilesOff}
	syscall.Syscall(sysIopolicysys, iopolCmdSet, uintptr(unsafe.Pointer(&p)), 0)
}

func lstatAt(_ int, base, name string, st *syscall.Stat_t) error { return syscall.Lstat(base+name, st) }

func fromStat(name string, st *syscall.Stat_t) rawEntry {
	e := modeEntry(name, uint32(st.Mode), st.Blocks, st.Mtimespec.Sec, uint64(uint32(st.Dev)), st.Ino, uint32(st.Nlink))
	e.cloud = st.Flags&sfDataless != 0
	return e
}

// "/" on APFS is the sealed system volume; user data lives on the Data volume and is
// grafted in through firmlinks (/Users, /Applications, /private…). Count that device as "ours"
// and skip the Data volume's own mount point so nothing is counted twice.
func extraRootDevices(root string) []string {
	if root == "/" {
		return []string{"/System/Volumes/Data"}
	}
	return nil
}

func defaultExcludes(root string) []string {
	if root != "/" {
		return nil
	}
	return []string{"/System/Volumes/Data", "/Volumes", "/dev", "/net", "/home"}
}
func statDev(st *syscall.Stat_t) uint64 { return uint64(uint32(st.Dev)) }

// APFS directory locks make kernel time climb steeply past ~8 concurrent readers with no wall-clock gain.
func defaultWorkers() int { return min(max(runtime.NumCPU(), 4), 8) }
