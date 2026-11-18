// mux lays a soundtrack under a film: the picture is copied as it is, the WAV encoded as AAC,
// and the two interleaved so that the file starts playing before it has all arrived.
//
//	swiftc -O -o mux mux.swift
//	mux film.mp4 track.wav out.mp4
import AVFoundation
import Foundation

func fail(_ s: String) -> Never {
    FileHandle.standardError.write("mux: \(s)\n".data(using: .utf8)!)
    exit(1)
}

let args = CommandLine.arguments
guard args.count == 4 else { fail("usage: mux film.mp4 track.wav out.mp4") }
let video = AVURLAsset(url: URL(fileURLWithPath: args[1]))
let audio = AVURLAsset(url: URL(fileURLWithPath: args[2]))
let outURL = URL(fileURLWithPath: args[3])
try? FileManager.default.removeItem(at: outURL)

guard let vtrack = try await video.loadTracks(withMediaType: .video).first else { fail("no picture in \(args[1])") }
guard let atrack = try await audio.loadTracks(withMediaType: .audio).first else { fail("no sound in \(args[2])") }
let vformat = try await vtrack.load(.formatDescriptions).first

let vreader = try AVAssetReader(asset: video)
let vout = AVAssetReaderTrackOutput(track: vtrack, outputSettings: nil)  // the samples as they are
vreader.add(vout)
let areader = try AVAssetReader(asset: audio)
let aout = AVAssetReaderTrackOutput(
    track: atrack,
    outputSettings: [
        AVFormatIDKey: kAudioFormatLinearPCM, AVLinearPCMBitDepthKey: 16, AVLinearPCMIsFloatKey: false,
        AVLinearPCMIsBigEndianKey: false, AVLinearPCMIsNonInterleaved: false,
    ])
areader.add(aout)

let writer = try AVAssetWriter(outputURL: outURL, fileType: .mp4)
writer.shouldOptimizeForNetworkUse = true
let vin = AVAssetWriterInput(mediaType: .video, outputSettings: nil, sourceFormatHint: vformat)
let ain = AVAssetWriterInput(
    mediaType: .audio,
    outputSettings: [
        AVFormatIDKey: kAudioFormatMPEG4AAC, AVSampleRateKey: 48000, AVNumberOfChannelsKey: 2, AVEncoderBitRateKey: 256_000,
    ])
vin.expectsMediaDataInRealTime = false
ain.expectsMediaDataInRealTime = false
writer.add(vin)
writer.add(ain)
guard vreader.startReading(), areader.startReading() else { fail("\(String(describing: vreader.error ?? areader.error))") }
guard writer.startWriting() else { fail(String(describing: writer.error)) }
writer.startSession(atSourceTime: .zero)

let group = DispatchGroup()
func pump(_ input: AVAssetWriterInput, _ out: AVAssetReaderTrackOutput, _ label: String) {
    group.enter()
    input.requestMediaDataWhenReady(on: DispatchQueue(label: label)) {
        while input.isReadyForMoreMediaData {
            guard let buf = out.copyNextSampleBuffer() else {
                input.markAsFinished()
                group.leave()
                return
            }
            if !input.append(buf) { fail("\(label): \(String(describing: writer.error))") }
        }
    }
}
pump(vin, vout, "picture")
pump(ain, aout, "sound")
group.wait()
await writer.finishWriting()
if writer.status != .completed { fail(String(describing: writer.error)) }
// the writer leaves the file it interleaved from beside the film
let dir = outURL.deletingLastPathComponent()
for name in (try? FileManager.default.contentsOfDirectory(atPath: dir.path)) ?? [] where name.hasPrefix(outURL.lastPathComponent + ".sb-") {
    try? FileManager.default.removeItem(at: dir.appendingPathComponent(name))
}
print("mux: \(outURL.path)")
