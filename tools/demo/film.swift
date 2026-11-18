// film composes the frames TestPromoVideo renders (app/promo_video_test.go) into the promo
// film and encodes it as H.264 with AVFoundation, so that it needs no ffmpeg. It knows nothing
// of the film: each frame comes with the list of layers to draw (the app's frame as the camera
// has it, type, rules, washes of light), which the test works out on its own clock.
//
//	swiftc -O -o film film.swift
//	film out.mp4 width height fps icon.png [sheets-dir [stills]]
//
// With a sheets directory it also writes contact sheets, a frame every half second, and the
// frames at the given times (seconds, comma separated) as PNG stills. TOSSY_DEMO_MBPS sets the
// average bit rate, in megabits a second at 1920×1080.
//
// Stdin is a stream of frames: a little-endian uint32 with the length of a JSON header (Meta),
// the header, then w×h RGBA pixels when it gives a size. Every measure of a layer is in the
// 1920×1080 canvas, from its top-left corner.
import AVFoundation
import AppKit
import CoreImage
import CoreText
import Foundation
import ImageIO
import UniformTypeIdentifiers

struct Layer: Decodable {
    var k: String  // win, text, rect, scrim, glow, icon
    var x: Double?
    var y: Double?
    var w: Double?
    var h: Double?
    var r: Double?  // corner radius
    var a: Double?  // opacity
    var c: [Double]?  // colour, 0 to 1
    var under: Bool?  // drawn under the window
    // win: the app's frame. z is how many canvas pixels a point of the window takes; (x, y) is
    // the window's top-left corner, or quad its four corners (tl, tr, br, bl) seen at an angle
    var z: Double?
    var quad: [Double]?
    var bloom: Double?
    // text: s, set with its baseline at y, from x (align l), about it (c) or up to it (r)
    var s: String?
    var size: Double?
    var wt: Double?  // weight, -1 to 1
    var mono: Bool?
    var digits: Bool?  // figures all of one width
    var align: String?
    var track: Double?
    var rev: Double?  // 0 to 1: the letters rise into the line one after another
    // scrim: a wash from opaque at one side (l, r, t, b) to clear at the other
    var side: String?
}

struct Meta: Decodable {
    var w: Int?  // the pixels that follow: a part of the frame, from (sx, sy)
    var h: Int?
    var sx: Int?
    var sy: Int?
    var fw: Int?  // the whole frame
    var fh: Int?
    var scale: Double?  // its pixels per point of the window
    var bg: [Double]?
    var layers: [Layer]?
    var flash: Double?  // a white frame over everything
    var dim: Double?  // a black one
    var vig: Double?  // how dark the corners go
}

let args = CommandLine.arguments
guard args.count >= 6, let W = Int(args[2]), let H = Int(args[3]), let fps = Int32(args[4]) else {
    FileHandle.standardError.write("usage: film out.mp4 width height fps icon.png [sheets-dir [stills]]\n".data(using: .utf8)!)
    exit(2)
}
let outURL = URL(fileURLWithPath: args[1])
let sheetsDir: String? = args.count > 6 ? args[6] : nil
let stills: [Int] = args.count > 7 ? args[7].split(separator: ",").compactMap { Double($0) }.map { Int(($0 * Double(fps)).rounded()) } : []
let S = CGFloat(W) / 1920
let srgb = CGColorSpace(name: CGColorSpace.sRGB)!
let canvas = CGRect(x: 0, y: 0, width: W, height: H)
// no colour management: every layer is sRGB and blends as Core Graphics blends
let ci = CIContext(options: [.workingColorSpace: NSNull(), .outputColorSpace: NSNull()])

func color(_ c: [Double]?, _ a: Double = 1) -> CGColor {
    let c = c ?? [1, 1, 1, 1]
    return CGColor(colorSpace: srgb, components: [CGFloat(c[0]), CGFloat(c[1]), CGFloat(c[2]), CGFloat((c.count > 3 ? c[3] : 1) * a)])!
}

let icon: CGImage? = {
    guard let src = CGImageSourceCreateWithURL(URL(fileURLWithPath: args[5]) as CFURL, nil) else { return nil }
    return CGImageSourceCreateImageAtIndex(src, 0, nil)
}()

// A drawing context the size of the canvas, flipped to a top-left origin and scaled so that
// what is drawn in it is measured on the 1920×1080 canvas.
func makeContext(_ w: Int, _ h: Int, scale: CGFloat) -> CGContext {
    let c = CGContext(
        data: nil, width: w, height: h, bitsPerComponent: 8, bytesPerRow: 0, space: srgb,
        bitmapInfo: CGImageAlphaInfo.premultipliedFirst.rawValue | CGBitmapInfo.byteOrder32Little.rawValue)!
    c.translateBy(x: 0, y: CGFloat(h))
    c.scaleBy(x: scale, y: -scale)
    return c
}

func draw(_ c: CGContext, _ img: CGImage, _ r: CGRect) {
    c.saveGState()
    c.translateBy(x: r.minX, y: r.maxY)
    c.scaleBy(x: 1, y: -1)
    c.draw(img, in: CGRect(x: 0, y: 0, width: r.width, height: r.height))
    c.restoreGState()
}

func easeOut(_ t: CGFloat) -> CGFloat { 1 - pow(1 - max(0, min(1, t)), 4) }

// ---------------------------------------------------------------------------------------------
// type

func font(_ l: Layer) -> CTFont {
    let size = CGFloat(l.size ?? 40)
    let weight = NSFont.Weight(CGFloat(l.wt ?? 0))
    if l.mono ?? false { return NSFont.monospacedSystemFont(ofSize: size, weight: weight) }
    if l.digits ?? false { return NSFont.monospacedDigitSystemFont(ofSize: size, weight: weight) }
    return NSFont.systemFont(ofSize: size, weight: weight)
}

// text sets a line. With rev under 1 its letters rise from below the line into place, one
// after another, behind a mask at the line's foot.
func text(_ c: CGContext, _ l: Layer) {
    guard let s = l.s, !s.isEmpty else { return }
    let alpha = CGFloat(l.a ?? 1)
    if alpha <= 0.001 { return }
    let f = font(l)
    let attrs: [NSAttributedString.Key: Any] = [
        NSAttributedString.Key(kCTFontAttributeName as String): f,
        NSAttributedString.Key(kCTKernAttributeName as String): CGFloat(l.track ?? 0),
        NSAttributedString.Key(kCTLanguageAttributeName as String): "zh-Hans",
    ]
    let line = CTLineCreateWithAttributedString(NSAttributedString(string: s, attributes: attrs))
    var ascent: CGFloat = 0, descent: CGFloat = 0, leading: CGFloat = 0
    let width = CGFloat(CTLineGetTypographicBounds(line, &ascent, &descent, &leading)) - CGFloat(l.track ?? 0)
    var x = CGFloat(l.x ?? 0)
    let y = CGFloat(l.y ?? 0)
    switch l.align ?? "l" {
    case "c": x -= width / 2
    case "r": x -= width
    default: break
    }
    let rev = CGFloat(l.rev ?? 1)
    let col = color(l.c, Double(alpha))
    c.saveGState()
    if rev < 0.999 {
        c.clip(to: CGRect(x: x - 20, y: y - ascent * 1.08, width: width + 40, height: (ascent + descent) * 1.1))
    }
    c.translateBy(x: x, y: y)
    c.scaleBy(x: 1, y: -1)
    c.setFillColor(col)
    let runs = CTLineGetGlyphRuns(line) as! [CTRun]
    var total = 0
    for run in runs { total += CTRunGetGlyphCount(run) }
    var i = 0
    let rise = (ascent + descent) * 1.05
    for run in runs {
        let n = CTRunGetGlyphCount(run)
        var glyphs = [CGGlyph](repeating: 0, count: n)
        var pos = [CGPoint](repeating: .zero, count: n)
        CTRunGetGlyphs(run, CFRange(location: 0, length: n), &glyphs)
        CTRunGetPositions(run, CFRange(location: 0, length: n), &pos)
        let rf = (CTRunGetAttributes(run) as NSDictionary)[kCTFontAttributeName as String] as! CTFont
        if rev >= 0.999 {
            CTFontDrawGlyphs(rf, glyphs, pos, n, c)
            i += n
            continue
        }
        for g in 0..<n {
            // each letter starts a little after the one before, and all are home when rev is 1
            let span: CGFloat = 0.55
            let delay = total > 1 ? CGFloat(i) / CGFloat(total - 1) * (1 - span) : 0
            let e = easeOut((rev - delay) / span)
            i += 1
            if e <= 0 { continue }
            var p = CGPoint(x: pos[g].x, y: pos[g].y - rise * (1 - e))
            var gl = glyphs[g]
            CTFontDrawGlyphs(rf, &gl, &p, 1, c)
        }
    }
    c.restoreGState()
}

// ---------------------------------------------------------------------------------------------
// the layers Core Graphics draws: all but the window

func paint(_ c: CGContext, _ l: Layer) {
    let a = l.a ?? 1
    if a <= 0.001 { return }
    switch l.k {
    case "text":
        text(c, l)
    case "rect":
        let r = CGRect(x: l.x ?? 0, y: l.y ?? 0, width: l.w ?? 0, height: l.h ?? 0)
        if r.width <= 0 || r.height <= 0 { return }
        let rad = min(CGFloat(l.r ?? 0), r.width / 2, r.height / 2)
        c.addPath(CGPath(roundedRect: r, cornerWidth: rad, cornerHeight: rad, transform: nil))
        c.setFillColor(color(l.c, a))
        c.fillPath()
    case "scrim":
        let r = CGRect(x: l.x ?? 0, y: l.y ?? 0, width: l.w ?? 1920, height: l.h ?? 1080)
        // eased, so that the wash has no edge where it ends
        var cols: [CGColor] = [], locs: [CGFloat] = []
        for i in 0...8 {
            let t = CGFloat(i) / 8
            cols.append(color(l.c ?? [0, 0, 0, 1], a * Double(pow(1 - t, 1.6))))
            locs.append(t)
        }
        let g = CGGradient(colorsSpace: srgb, colors: cols as CFArray, locations: locs)!
        var p0 = CGPoint(x: r.minX, y: r.midY), p1 = CGPoint(x: r.maxX, y: r.midY)
        switch l.side ?? "l" {
        case "r": (p0, p1) = (p1, p0)
        case "t": (p0, p1) = (CGPoint(x: r.midX, y: r.minY), CGPoint(x: r.midX, y: r.maxY))
        case "b": (p0, p1) = (CGPoint(x: r.midX, y: r.maxY), CGPoint(x: r.midX, y: r.minY))
        default: break
        }
        c.saveGState()
        c.clip(to: r)
        c.drawLinearGradient(g, start: p0, end: p1, options: [])
        c.restoreGState()
    case "glow":
        // a soft light: an ellipse of w×h about (x, y)
        let rx = CGFloat(l.w ?? 400), ry = CGFloat(l.h ?? 400)
        var cols: [CGColor] = [], locs: [CGFloat] = []
        for i in 0...8 {
            let t = CGFloat(i) / 8
            cols.append(color(l.c, a * Double(pow(1 - t, 2))))
            locs.append(t)
        }
        let g = CGGradient(colorsSpace: srgb, colors: cols as CFArray, locations: locs)!
        c.saveGState()
        c.translateBy(x: CGFloat(l.x ?? 960), y: CGFloat(l.y ?? 540))
        c.scaleBy(x: 1, y: ry / rx)
        c.drawRadialGradient(g, startCenter: .zero, startRadius: 0, endCenter: .zero, endRadius: rx, options: [])
        c.restoreGState()
    case "icon":
        guard let icon = icon else { return }
        let d = CGFloat(l.size ?? 180)
        c.saveGState()
        c.setAlpha(CGFloat(a))
        c.interpolationQuality = .high
        draw(c, icon, CGRect(x: CGFloat(l.x ?? 960) - d / 2, y: CGFloat(l.y ?? 540) - d / 2, width: d, height: d))
        c.restoreGState()
    default:
        break
    }
}

func overlay(_ layers: [Layer]) -> CIImage? {
    if layers.isEmpty { return nil }
    let c = makeContext(W, H, scale: S)
    for l in layers { paint(c, l) }
    guard let img = c.makeImage() else { return nil }
    return CIImage(cgImage: img, options: [.colorSpace: NSNull()])
}

// ---------------------------------------------------------------------------------------------
// the window: the app's frame, as the camera has it

// window places the part of the frame that came (part, at (sx, sy) of a frame of fw×fh pixels,
// scale to a point) on the canvas.
func window(_ l: Layer, _ m: Meta, _ part: CIImage) -> CIImage {
    let src = CGFloat(m.scale ?? 1)
    let fw = CGFloat(m.fw ?? Int(part.extent.width)), fh = CGFloat(m.fh ?? Int(part.extent.height))
    let z = CGFloat(l.z ?? 1) * S  // output pixels per point
    let k = z / src
    // into the whole frame's own space, origin bottom-left
    var img = part.transformed(by: CGAffineTransform(translationX: CGFloat(m.sx ?? 0), y: fh - CGFloat(m.sy ?? 0) - part.extent.height))
    if k < 0.999 {
        img = img.applyingFilter("CILanczosScaleTransform", parameters: [kCIInputScaleKey: k, kCIInputAspectRatioKey: 1])
    } else if k > 1.001 {
        img = img.transformed(by: CGAffineTransform(scaleX: k, y: k), highQualityDownsample: false)
    }
    let full = CGRect(x: 0, y: 0, width: fw * k, height: fh * k)
    let shown = img.extent.intersection(full)
    if let b = l.bloom, b > 0.001 {
        img = img.clampedToExtent().applyingFilter("CIBloom", parameters: [kCIInputRadiusKey: 9 * z, kCIInputIntensityKey: b]).cropped(to: shown)
    }
    if let r = l.r, r > 0 {
        // a slab: rounded corners and a hairline of light along its edge
        let w = Int(full.width.rounded()), h = Int(full.height.rounded())
        let rad = CGFloat(r) * z
        let shape = CGPath(roundedRect: CGRect(x: 0, y: 0, width: w, height: h), cornerWidth: rad, cornerHeight: rad, transform: nil)
        let edge = makeContext(w, h, scale: 1)
        edge.addPath(CGPath(roundedRect: CGRect(x: 0.75, y: 0.75, width: CGFloat(w) - 1.5, height: CGFloat(h) - 1.5), cornerWidth: rad, cornerHeight: rad, transform: nil))
        edge.setStrokeColor(CGColor(colorSpace: srgb, components: [1, 1, 1, 0.2])!)
        edge.setLineWidth(1.5)
        edge.strokePath()
        let mask = makeContext(w, h, scale: 1)
        mask.addPath(shape)
        mask.setFillColor(CGColor(colorSpace: srgb, components: [1, 1, 1, 1])!)
        mask.fillPath()
        if let e = edge.makeImage(), let mk = mask.makeImage() {
            img = CIImage(cgImage: e, options: [.colorSpace: NSNull()]).composited(over: img)
            img = img.applyingFilter("CIBlendWithAlphaMask", parameters: [
                kCIInputBackgroundImageKey: CIImage.empty(), kCIInputMaskImageKey: CIImage(cgImage: mk, options: [.colorSpace: NSNull()]),
            ])
        }
    }
    if let q = l.quad, q.count == 8 {
        // seen at an angle: the frame's corners go to the quad's
        func pt(_ i: Int) -> CIVector { CIVector(x: CGFloat(q[i]) * S, y: CGFloat(H) - CGFloat(q[i + 1]) * S) }
        img = img.cropped(to: full).applyingFilter("CIPerspectiveTransform", parameters: [
            "inputTopLeft": pt(0), "inputTopRight": pt(2), "inputBottomRight": pt(4), "inputBottomLeft": pt(6),
        ])
    } else {
        img = img.transformed(by: CGAffineTransform(translationX: CGFloat(l.x ?? 0) * S, y: CGFloat(H) - CGFloat(l.y ?? 0) * S - full.height))
    }
    if let a = l.a, a < 0.999 {
        img = img.applyingFilter("CIColorMatrix", parameters: [
            "inputRVector": CIVector(x: CGFloat(a), y: 0, z: 0, w: 0), "inputGVector": CIVector(x: 0, y: CGFloat(a), z: 0, w: 0),
            "inputBVector": CIVector(x: 0, y: 0, z: CGFloat(a), w: 0), "inputAVector": CIVector(x: 0, y: 0, z: 0, w: CGFloat(a)),
        ])
    }
    return img
}

// the corners, darkened: drawn once
let vignette: CIImage = {
    let c = makeContext(W, H, scale: S)
    var cols: [CGColor] = [], locs: [CGFloat] = []
    for i in 0...8 {
        let t = CGFloat(i) / 8
        cols.append(CGColor(colorSpace: srgb, components: [0, 0, 0, pow(t, 2.4)])!)
        locs.append(t)
    }
    let g = CGGradient(colorsSpace: srgb, colors: cols as CFArray, locations: locs)!
    c.saveGState()
    c.translateBy(x: 960, y: 540)
    c.scaleBy(x: 1, y: 0.62)
    c.drawRadialGradient(g, startCenter: .zero, startRadius: 420, endCenter: .zero, endRadius: 1240, options: [.drawsAfterEndLocation])
    c.restoreGState()
    return CIImage(cgImage: c.makeImage()!, options: [.colorSpace: NSNull()])
}()

func wash(_ c: [CGFloat], _ a: Double) -> CIImage {
    CIImage(color: CIColor(red: c[0] * CGFloat(a), green: c[1] * CGFloat(a), blue: c[2] * CGFloat(a), alpha: CGFloat(a))).cropped(to: canvas)
}

func compose(_ m: Meta, _ part: CIImage?) -> CIImage {
    let bg = m.bg ?? [0, 0, 0]
    var out = CIImage(color: CIColor(red: CGFloat(bg[0]), green: CGFloat(bg[1]), blue: CGFloat(bg[2]))).cropped(to: canvas)
    let layers = m.layers ?? []
    if let u = overlay(layers.filter { $0.k != "win" && ($0.under ?? false) }) { out = u.composited(over: out) }
    if let part = part {
        for l in layers where l.k == "win" { out = window(l, m, part).composited(over: out) }
    }
    if let v = m.vig, v > 0.001 {
        out = vignette.applyingFilter("CIColorMatrix", parameters: ["inputAVector": CIVector(x: 0, y: 0, z: 0, w: CGFloat(v))]).composited(over: out)
    }
    if let d = m.dim, d > 0.001 { out = wash([0, 0, 0], d).composited(over: out) }
    if let o = overlay(layers.filter { $0.k != "win" && !($0.under ?? false) }) { out = o.composited(over: out) }
    if let f = m.flash, f > 0.001 { out = wash([1, 1, 1], f).composited(over: out) }
    return out.cropped(to: canvas)
}

// ---------------------------------------------------------------------------------------------
// reading stdin, writing the film

func readFull(_ buf: UnsafeMutableRawPointer, _ n: Int) -> Bool {
    var got = 0
    while got < n {
        let k = fread(buf + got, 1, n - got, stdin)
        if k <= 0 { return false }
        got += k
    }
    return true
}

func fail(_ s: String) -> Never {
    FileHandle.standardError.write("film: \(s)\n".data(using: .utf8)!)
    exit(1)
}

try? FileManager.default.removeItem(at: outURL)
let writer = try AVAssetWriter(outputURL: outURL, fileType: .mp4)
let mbps = Double(ProcessInfo.processInfo.environment["TOSSY_DEMO_MBPS"] ?? "") ?? 10
let input = AVAssetWriterInput(
    mediaType: .video,
    outputSettings: [
        AVVideoCodecKey: AVVideoCodecType.h264,
        AVVideoWidthKey: W,
        AVVideoHeightKey: H,
        AVVideoColorPropertiesKey: [
            AVVideoColorPrimariesKey: AVVideoColorPrimaries_ITU_R_709_2,
            AVVideoTransferFunctionKey: AVVideoTransferFunction_ITU_R_709_2,
            AVVideoYCbCrMatrixKey: AVVideoYCbCrMatrix_ITU_R_709_2,
        ],
        AVVideoCompressionPropertiesKey: [
            AVVideoAverageBitRateKey: Int(mbps * 1_000_000 * Double(S * S)),
            AVVideoProfileLevelKey: AVVideoProfileLevelH264HighAutoLevel,
            AVVideoMaxKeyFrameIntervalKey: Int(fps) * 2,
            AVVideoExpectedSourceFrameRateKey: Int(fps),
        ],
    ])
input.expectsMediaDataInRealTime = false
let adaptor = AVAssetWriterInputPixelBufferAdaptor(
    assetWriterInput: input,
    sourcePixelBufferAttributes: [
        kCVPixelBufferPixelFormatTypeKey as String: kCVPixelFormatType_32BGRA,
        kCVPixelBufferWidthKey as String: W,
        kCVPixelBufferHeightKey as String: H,
        kCVPixelBufferMetalCompatibilityKey as String: true,
    ])
writer.add(input)
guard writer.startWriting() else { fail(String(describing: writer.error)) }
writer.startSession(atSourceTime: .zero)

// contact sheets, to look a render over without playing it: every half second, nine to a sheet
let thumbW = 640, thumbH = 360, perSheet = 9
var sheet: CGContext? = nil
var sheetN = 0, thumbN = 0
func save(_ img: CGImage, _ name: String, _ type: UTType) {
    guard let dir = sheetsDir else { return }
    let url = URL(fileURLWithPath: dir).appendingPathComponent(name)
    if let dst = CGImageDestinationCreateWithURL(url as CFURL, type.identifier as CFString, 1, nil) {
        CGImageDestinationAddImage(dst, img, [kCGImageDestinationLossyCompressionQuality: 0.85] as CFDictionary)
        CGImageDestinationFinalize(dst)
    }
}
func flushSheet() {
    guard let s = sheet, let img = s.makeImage() else { return }
    save(img, String(format: "sheet-%03d.jpg", sheetN), .jpeg)
    sheetN += 1
    sheet = nil
}
func addThumb(_ img: CGImage, frame: Int) {
    if sheet == nil { sheet = makeContext(thumbW * 3, thumbH * 3, scale: 1) }
    let i = thumbN % perSheet
    let r = CGRect(x: (i % 3) * thumbW, y: (i / 3) * thumbH, width: thumbW, height: thumbH)
    sheet!.interpolationQuality = .high
    draw(sheet!, img, r)
    var label = Layer(k: "text")
    label.s = String(format: "%.1fs", Double(frame) / Double(fps))
    label.x = Double(r.minX) + 12
    label.y = Double(r.minY) + 24
    label.size = 16
    label.wt = 0.4
    label.c = [1, 0.86, 0.35, 1]
    text(sheet!, label)
    thumbN += 1
    if thumbN % perSheet == 0 { flushSheet() }
}

var part: CIImage? = nil
var n = 0
var lenBuf = [UInt8](repeating: 0, count: 4)
let decoder = JSONDecoder()
while readFull(&lenBuf, 4) {
    let len = Int(lenBuf[0]) | Int(lenBuf[1]) << 8 | Int(lenBuf[2]) << 16 | Int(lenBuf[3]) << 24
    var hdr = [UInt8](repeating: 0, count: len)
    guard readFull(&hdr, len) else { break }
    let m: Meta
    do { m = try decoder.decode(Meta.self, from: Data(hdr)) } catch { fail("frame \(n): \(error)") }
    if let w = m.w, let h = m.h, w > 0, h > 0 {
        var data = Data(count: w * h * 4)
        let ok = data.withUnsafeMutableBytes { readFull($0.baseAddress!, w * h * 4) }
        guard ok else { break }
        // RGBA, its first row at the top, which is how Core Image reads bitmap data
        part = CIImage(bitmapData: data, bytesPerRow: w * 4, size: CGSize(width: w, height: h), format: .RGBA8, colorSpace: nil)
    } else if !(m.layers ?? []).contains(where: { $0.k == "win" }) {
        part = nil
    }
    while !input.isReadyForMoreMediaData { usleep(2000) }
    var pb: CVPixelBuffer? = nil
    CVPixelBufferPoolCreatePixelBuffer(nil, adaptor.pixelBufferPool!, &pb)
    guard let buf = pb else { break }
    let img = compose(m, part)
    ci.render(img, to: buf, bounds: canvas, colorSpace: nil)
    if sheetsDir != nil, n % (Int(fps) / 2) == 0 || stills.contains(n), let cg = ci.createCGImage(img, from: canvas, format: .RGBA8, colorSpace: srgb) {
        if n % (Int(fps) / 2) == 0 { addThumb(cg, frame: n) }
        if stills.contains(n) { save(cg, String(format: "still-%05.1f.png", Double(n) / Double(fps)), .png) }
    }
    if !adaptor.append(buf, withPresentationTime: CMTime(value: CMTimeValue(n), timescale: fps)) {
        fail("append: \(String(describing: writer.error))")
    }
    n += 1
}
flushSheet()
input.markAsFinished()
let done = DispatchSemaphore(value: 0)
writer.finishWriting { done.signal() }
done.wait()
if writer.status != .completed { fail(String(describing: writer.error)) }
print("film: \(n) frames, \(String(format: "%.1f", Double(n) / Double(fps)))s → \(outURL.path)")
