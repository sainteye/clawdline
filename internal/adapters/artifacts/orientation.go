package artifacts

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
)

// exifOrientation is the EXIF orientation tag (1…8) of an encoded picture, or
// 1 when it has none or the one it has cannot be read. A JPEG carries it in
// its APP1 segment; a PNG in an eXIf chunk, which is where `sips` leaves a
// HEIC's tag when it converts one. Only the tag is read: nothing else from the
// metadata is kept.
func exifOrientation(raw []byte, format string) int {
	var tiff []byte
	switch format {
	case "jpeg":
		tiff = jpegEXIF(raw)
	case "png":
		tiff = pngEXIF(raw)
	}
	return tiffOrientation(tiff)
}

// jpegEXIF is the TIFF body of the first `Exif` APP1 segment before the image
// data starts, or nil.
func jpegEXIF(raw []byte) []byte {
	if len(raw) < 4 || raw[0] != 0xFF || raw[1] != 0xD8 {
		return nil
	}
	for i := 2; i+4 <= len(raw); {
		if raw[i] != 0xFF {
			return nil
		}
		marker := raw[i+1]
		if marker == 0xD8 || marker >= 0xD0 && marker <= 0xD7 || marker == 0x01 || marker == 0xFF {
			i++
			continue
		}
		if marker == 0xDA || marker == 0xD9 {
			return nil
		}
		n := int(binary.BigEndian.Uint16(raw[i+2:]))
		if n < 2 || i+2+n > len(raw) {
			return nil
		}
		body := raw[i+4 : i+2+n]
		if marker == 0xE1 && bytes.HasPrefix(body, []byte("Exif\x00\x00")) {
			return body[6:]
		}
		i += 2 + n
	}
	return nil
}

// pngEXIF is the body of the first eXIf chunk before the image data, or nil.
func pngEXIF(raw []byte) []byte {
	const signature = "\x89PNG\r\n\x1a\n"
	if !bytes.HasPrefix(raw, []byte(signature)) {
		return nil
	}
	for i := len(signature); i+8 <= len(raw); {
		n := int(binary.BigEndian.Uint32(raw[i:]))
		kind := string(raw[i+4 : i+8])
		if n < 0 || i+8+n > len(raw) || kind == "IDAT" || kind == "IEND" {
			return nil
		}
		if kind == "eXIf" {
			return raw[i+8 : i+8+n]
		}
		i += 12 + n
	}
	return nil
}

// tiffOrientation reads tag 0x0112 from the first IFD of a TIFF body.
func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	ifd := int(order.Uint32(tiff[4:]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return 1
	}
	count := int(order.Uint16(tiff[ifd:]))
	for k := 0; k < count; k++ {
		e := ifd + 2 + 12*k
		if e+12 > len(tiff) {
			return 1
		}
		if order.Uint16(tiff[e:]) != 0x0112 {
			continue
		}
		// SHORT, one value, stored in the first two bytes of the value field.
		if order.Uint16(tiff[e+2:]) != 3 {
			return 1
		}
		if o := int(order.Uint16(tiff[e+8:])); o >= 1 && o <= 8 {
			return o
		}
		return 1
	}
	return 1
}

// orient draws img upright for an EXIF orientation, so a reader that ignores
// the tag — a terminal program reading the file, a stored picture that no
// longer carries it — sees what the camera's owner saw. Orientation 1, or one
// that is not a valid tag, is img unchanged.
func orient(img image.Image, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	src, ok := img.(*image.RGBA)
	if !ok || b.Min != (image.Point{}) {
		src = image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	}
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Where the pixel at (x, y) of the stored picture is drawn.
			var tx, ty int
			switch orientation {
			case 2: // mirrored
				tx, ty = w-1-x, y
			case 3: // upside down
				tx, ty = w-1-x, h-1-y
			case 4: // mirrored upside down
				tx, ty = x, h-1-y
			case 5: // mirrored across the diagonal
				tx, ty = y, x
			case 6: // turned a quarter clockwise
				tx, ty = h-1-y, x
			case 7: // mirrored across the other diagonal
				tx, ty = h-1-y, w-1-x
			case 8: // turned a quarter anticlockwise
				tx, ty = y, w-1-x
			}
			copy(dst.Pix[dst.PixOffset(tx, ty):][:4], src.Pix[src.PixOffset(x, y):][:4])
		}
	}
	return dst
}
