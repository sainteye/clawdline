import AppKit
import CoreImage
import Foundation

/// A key, as something a phone can look at.
///
/// The alternative is typing a forty-three character token on a phone keyboard, which nobody will
/// do twice and most people will not do once — and the fallback to that, a six-digit code read off
/// the Mac, still costs a walk to the machine. A camera does it in a second.
///
/// **What is in the picture is a live credential**, and that shapes everything here. It is drawn
/// on demand and never written to disk, the window that shows it is not resizable and not
/// screenshot-friendly by accident, and the token in it is minted for that one scan — so a photo
/// of somebody's screen is a device you can see in Settings and revoke, rather than the machine's
/// own key.
///
/// `CIQRCodeGenerator` is in Core Image, which is in the OS. No dependency, and no encoder here
/// to be wrong about error correction levels.
enum RemoteQR {

    /// `M` — about 15% of the code can be obscured and it still reads.
    ///
    /// Not `L`, which is smaller but gives up on a fingerprint or a reflection, and not `H`, which
    /// would make the modules small enough on a 260-point square that an older phone camera starts
    /// missing them. This is a thing being photographed off a glossy screen at arm's length.
    private static let correction = "M"

    static func image(for text: String, side: CGFloat) -> NSImage? {
        guard let filter = CIFilter(name: "CIQRCodeGenerator") else { return nil }
        filter.setValue(Data(text.utf8), forKey: "inputMessage")
        filter.setValue(correction, forKey: "inputCorrectionLevel")
        guard let code = filter.outputImage else { return nil }

        // Scaled by a whole number and drawn without interpolation. A QR code is squares, and a
        // resampled square has a soft edge that a camera has to guess at — the one thing that
        // makes a code that should read at arm's length need a second try.
        let scale = max(1, floor(side / code.extent.width))
        let scaled = code.transformed(by: CGAffineTransform(scaleX: scale, y: scale))
        let context = CIContext(options: [.useSoftwareRenderer: false])
        guard let cg = context.createCGImage(scaled, from: scaled.extent) else { return nil }
        return NSImage(cgImage: cg, size: NSSize(width: side, height: side))
    }

    /// The address to put in the code, with a token already in it.
    ///
    /// The public one when there is one, because **a phone cannot reach `127.0.0.1`** — a code
    /// that works beautifully on the Mac that drew it and nowhere else is the obvious mistake
    /// here, and it would only be discovered by somebody standing in a kitchen.
    static func signInURL(token: String, hostname: String, tunnel: RemoteTunnel.State,
                          port: Int) -> String {
        if case .up(let url) = tunnel, !url.isEmpty {
            return "\(url.hasSuffix("/") ? String(url.dropLast()) : url)/?t=\(token)"
        }
        let host = hostname.trimmingCharacters(in: .whitespaces)
        if !host.isEmpty { return "https://\(host)/?t=\(token)" }
        return "http://127.0.0.1:\(port)/?t=\(token)"
    }
}
