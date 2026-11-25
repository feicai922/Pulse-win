package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
)

const (
	rtIcon      uint32 = 3
	rtGroupIcon uint32 = 14
	rtManifest  uint32 = 24
	langENUS    uint32 = 1033

	imageFileMachineAMD64   = 0x8664
	imageScnCntInitData     = 0x00000040
	imageScnMemRead         = 0x40000000
	imageRelAMD64Addr32NB   = 3
	imageSymClassStatic     = 3
)

const appManifestXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity version="1.0.0.0" processorArchitecture="amd64" name="Pulse.Monitor" type="win32"/>
  <description>Pulse AI Quota Monitor</description>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
    </application>
  </compatibility>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2</dpiAwareness>
    </windowsSettings>
  </application>
</assembly>
`

type iconEntry struct {
	size int
	data []byte
}

func main() {
	srcPng := flag.String("png", filepath.FromSlash("../AppIcon/pulse-icon-1024.png"), "source 1024px PNG icon")
	outIco := flag.String("ico", filepath.FromSlash("assets/pulse.ico"), "output .ico path")
	outSyso := flag.String("syso", "rsrc_windows_amd64.syso", "output .syso paths (comma-separated allowed via multiple calls)")
	outSyso2 := flag.String("syso2", filepath.FromSlash("cmd/installer/rsrc_windows_amd64.syso"), "optional second .syso path")
	flag.Parse()

	f, err := os.Open(*srcPng)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open png: %v\n", err)
		os.Exit(1)
	}
	srcImg, err := png.Decode(f)
	_ = f.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "decode png: %v\n", err)
		os.Exit(1)
	}

	sizes := []int{16, 24, 32, 48, 64, 256}
	entries := make([]iconEntry, 0, len(sizes))
	for _, sz := range sizes {
		resized := resizeBox(srcImg, sz, sz)
		var payload []byte
		if sz >= 256 {
			var buf bytes.Buffer
			if err := png.Encode(&buf, resized); err != nil {
				fmt.Fprintf(os.Stderr, "encode png %d: %v\n", sz, err)
				os.Exit(1)
			}
			payload = buf.Bytes()
		} else {
			payload = encodeBMPDIBForIcon(resized)
		}
		entries = append(entries, iconEntry{size: sz, data: payload})
	}

	if *outIco != "" {
		icoBytes := buildICO(entries)
		if err := os.MkdirAll(filepath.Dir(*outIco), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir ico: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*outIco, icoBytes, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write ico: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s (%d bytes)\n", *outIco, len(icoBytes))
	}

	sysoBytes := buildCOFFRsrc(entries, []byte(appManifestXML))
	for _, dst := range []string{*outSyso, *outSyso2} {
		if dst == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir syso: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(dst, sysoBytes, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write syso: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s (%d bytes)\n", dst, len(sysoBytes))
	}
}

// resizeBox performs straight-alpha area-averaging downsampling for crisp icons.
func resizeBox(src image.Image, dstW, dstH int) *image.NRGBA {
	b := src.Bounds()
	srcW := b.Dx()
	srcH := b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))

	for dy := 0; dy < dstH; dy++ {
		sy0 := float64(dy) * float64(srcH) / float64(dstH)
		sy1 := float64(dy+1) * float64(srcH) / float64(dstH)
		yStart := int(sy0)
		yEnd := int(sy1)
		if float64(yEnd) < sy1 {
			yEnd++
		}
		if yEnd > srcH {
			yEnd = srcH
		}

		for dx := 0; dx < dstW; dx++ {
			sx0 := float64(dx) * float64(srcW) / float64(dstW)
			sx1 := float64(dx+1) * float64(srcW) / float64(dstW)
			xStart := int(sx0)
			xEnd := int(sx1)
			if float64(xEnd) < sx1 {
				xEnd++
			}
			if xEnd > srcW {
				xEnd = srcW
			}

			var sumR, sumG, sumB, sumA, sumW float64
			for sy := yStart; sy < yEnd; sy++ {
				wy := minFloat(sy1, float64(sy+1)) - maxFloat(sy0, float64(sy))
				if wy <= 0 {
					continue
				}
				for sx := xStart; sx < xEnd; sx++ {
					wx := minFloat(sx1, float64(sx+1)) - maxFloat(sx0, float64(sx))
					if wx <= 0 {
						continue
					}
					w := wx * wy
					r, g, bl, a := src.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					// RGBA() returns premultiplied 16-bit values.
					sumR += float64(r) * w
					sumG += float64(g) * w
					sumB += float64(bl) * w
					sumA += float64(a) * w
					sumW += w
				}
			}

			if sumW > 0 {
				a := sumA / sumW
				var nr, ng, nb uint8
				na := uint8(clamp255(a / 257.0))
				if a > 0 {
					// Un-premultiply back to NRGBA
					nr = uint8(clamp255((sumR / sumW) * 65535.0 / a / 257.0))
					ng = uint8(clamp255((sumG / sumW) * 65535.0 / a / 257.0))
					nb = uint8(clamp255((sumB / sumW) * 65535.0 / a / 257.0))
				}
				off := dst.PixOffset(dx, dy)
				dst.Pix[off+0] = nr
				dst.Pix[off+1] = ng
				dst.Pix[off+2] = nb
				dst.Pix[off+3] = na
			}
		}
	}
	return dst
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func clamp255(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v + 0.5
}

func encodeBMPDIBForIcon(img *image.NRGBA) []byte {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	andStride := ((w + 31) / 32) * 4
	xorSize := w * h * 4
	andSize := andStride * h

	buf := new(bytes.Buffer)
	// BITMAPINFOHEADER (40 bytes)
	_ = binary.Write(buf, binary.LittleEndian, uint32(40))
	_ = binary.Write(buf, binary.LittleEndian, int32(w))
	_ = binary.Write(buf, binary.LittleEndian, int32(h*2)) // XOR + AND height
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))  // Planes
	_ = binary.Write(buf, binary.LittleEndian, uint16(32)) // BitCount
	_ = binary.Write(buf, binary.LittleEndian, uint32(0))  // BI_RGB
	_ = binary.Write(buf, binary.LittleEndian, uint32(xorSize))
	_ = binary.Write(buf, binary.LittleEndian, int32(0))
	_ = binary.Write(buf, binary.LittleEndian, int32(0))
	_ = binary.Write(buf, binary.LittleEndian, uint32(0))
	_ = binary.Write(buf, binary.LittleEndian, uint32(0))

	// Bottom-up BGRA pixels
	row := make([]byte, w*4)
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			off := img.PixOffset(x, y)
			row[x*4+0] = img.Pix[off+2] // B
			row[x*4+1] = img.Pix[off+1] // G
			row[x*4+2] = img.Pix[off+0] // R
			row[x*4+3] = img.Pix[off+3] // A
		}
		buf.Write(row)
	}
	// AND mask (zeros; 32bpp alpha handles mask, 1 for fully transparent)
	andMask := make([]byte, andSize)
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			if img.Pix[img.PixOffset(x, y)+3] == 0 {
				rowIdx := (h - 1 - y) * andStride
				andMask[rowIdx+x/8] |= 1 << (7 - uint(x%8))
			}
		}
	}
	buf.Write(andMask)
	return buf.Bytes()
}

func buildICO(entries []iconEntry) []byte {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, uint16(0))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(buf, binary.LittleEndian, uint16(len(entries)))

	offset := uint32(6 + len(entries)*16)
	for _, e := range entries {
		dim := byte(e.size)
		if e.size >= 256 {
			dim = 0
		}
		buf.WriteByte(dim)
		buf.WriteByte(dim)
		buf.WriteByte(0)
		buf.WriteByte(0)
		_ = binary.Write(buf, binary.LittleEndian, uint16(1))
		_ = binary.Write(buf, binary.LittleEndian, uint16(32))
		_ = binary.Write(buf, binary.LittleEndian, uint32(len(e.data)))
		_ = binary.Write(buf, binary.LittleEndian, offset)
		offset += uint32(len(e.data))
	}
	for _, e := range entries {
		buf.Write(e.data)
	}
	return buf.Bytes()
}

func buildGroupIconResource(entries []iconEntry) []byte {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, uint16(0))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(buf, binary.LittleEndian, uint16(len(entries)))
	for i, e := range entries {
		dim := byte(e.size)
		if e.size >= 256 {
			dim = 0
		}
		buf.WriteByte(dim)
		buf.WriteByte(dim)
		buf.WriteByte(0)
		buf.WriteByte(0)
		_ = binary.Write(buf, binary.LittleEndian, uint16(1))
		_ = binary.Write(buf, binary.LittleEndian, uint16(32))
		_ = binary.Write(buf, binary.LittleEndian, uint32(len(e.data)))
		_ = binary.Write(buf, binary.LittleEndian, uint16(i+1))
	}
	return buf.Bytes()
}

type resLeaf struct {
	typ  uint32
	id   uint32
	lang uint32
	data []byte
}

func buildCOFFRsrc(entries []iconEntry, manifest []byte) []byte {
	var leaves []resLeaf
	for i, e := range entries {
		leaves = append(leaves, resLeaf{
			typ:  rtIcon,
			id:   uint32(i + 1),
			lang: langENUS,
			data: e.data,
		})
	}
	leaves = append(leaves, resLeaf{
		typ:  rtGroupIcon,
		id:   1,
		lang: langENUS,
		data: buildGroupIconResource(entries),
	})
	if len(manifest) > 0 {
		leaves = append(leaves, resLeaf{
			typ:  rtManifest,
			id:   1,
			lang: langENUS,
			data: manifest,
		})
	}

	// Group leaves by type -> id -> lang (all sorted by ID).
	typeMap := map[uint32]map[uint32][]resLeaf{}
	var types []uint32
	for _, lf := range leaves {
		if _, ok := typeMap[lf.typ]; !ok {
			typeMap[lf.typ] = map[uint32][]resLeaf{}
			types = append(types, lf.typ)
		}
		typeMap[lf.typ][lf.id] = append(typeMap[lf.typ][lf.id], lf)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })

	idsByType := map[uint32][]uint32{}
	totalIDDirs := 0
	totalLangDirs := 0
	for _, t := range types {
		var ids []uint32
		for id := range typeMap[t] {
			ids = append(ids, id)
			sort.Slice(typeMap[t][id], func(i, j int) bool {
				return typeMap[t][id][i].lang < typeMap[t][id][j].lang
			})
			totalLangDirs++
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		idsByType[t] = ids
		totalIDDirs += len(ids)
	}

	// Compute directory table sizes:
	// Directory header = 16 bytes, each directory entry = 8 bytes.
	const dirHdrSize = 16
	const dirEntSize = 8
	const dataEntSize = 16

	typeDirSize := dirHdrSize + len(types)*dirEntSize
	idDirsOffset := typeDirSize
	langDirsOffset := idDirsOffset + totalIDDirs*dirHdrSize + len(leaves)*dirEntSize // wait: let's compute exact sizes!
	_ = langDirsOffset

	// Calculate exact offset of every level-2 directory, level-3 directory, and data entry.
	cur := typeDirSize
	idDirOff := map[uint32]int{}
	for _, t := range types {
		idDirOff[t] = cur
		cur += dirHdrSize + len(idsByType[t])*dirEntSize
	}
	type leafKey struct{ typ, id uint32 }
	langDirOff := map[leafKey]int{}
	for _, t := range types {
		for _, id := range idsByType[t] {
			langDirOff[leafKey{t, id}] = cur
			cur += dirHdrSize + len(typeMap[t][id])*dirEntSize
		}
	}
	dataEntriesOff := cur
	totalLeaves := len(leaves)
	payloadStart := (dataEntriesOff + totalLeaves*dataEntSize + 7) &^ 7

	type reloc struct {
		vaddr uint32
	}
	var relocs []reloc

	rsrc := new(bytes.Buffer)
	writeDirHeader := func(idCount int) {
		_ = binary.Write(rsrc, binary.LittleEndian, uint32(0))         // Characteristics
		_ = binary.Write(rsrc, binary.LittleEndian, uint32(0))         // TimeDateStamp
		_ = binary.Write(rsrc, binary.LittleEndian, uint16(0))         // MajorVersion
		_ = binary.Write(rsrc, binary.LittleEndian, uint16(0))         // MinorVersion
		_ = binary.Write(rsrc, binary.LittleEndian, uint16(0))         // NumberOfNamedEntries
		_ = binary.Write(rsrc, binary.LittleEndian, uint16(idCount))   // NumberOfIdEntries
	}

	// Level 1: Types
	writeDirHeader(len(types))
	for _, t := range types {
		_ = binary.Write(rsrc, binary.LittleEndian, t)
		_ = binary.Write(rsrc, binary.LittleEndian, uint32(0x80000000|idDirOff[t]))
	}

	// Level 2: IDs
	for _, t := range types {
		ids := idsByType[t]
		writeDirHeader(len(ids))
		for _, id := range ids {
			_ = binary.Write(rsrc, binary.LittleEndian, id)
			_ = binary.Write(rsrc, binary.LittleEndian, uint32(0x80000000|langDirOff[leafKey{t, id}]))
		}
	}

	// Level 3: Languages -> DataEntry offsets
	leafDataEntOff := make([]int, 0, totalLeaves)
	leafOrder := make([]resLeaf, 0, totalLeaves)
	nextDataEnt := dataEntriesOff
	for _, t := range types {
		for _, id := range idsByType[t] {
			items := typeMap[t][id]
			writeDirHeader(len(items))
			for _, lf := range items {
				_ = binary.Write(rsrc, binary.LittleEndian, lf.lang)
				_ = binary.Write(rsrc, binary.LittleEndian, uint32(nextDataEnt))
				leafDataEntOff = append(leafDataEntOff, nextDataEnt)
				leafOrder = append(leafOrder, lf)
				nextDataEnt += dataEntSize
			}
		}
	}

	// Data entries + compute payload offsets
	payloadOffs := make([]int, len(leafOrder))
	pCur := payloadStart
	for i, lf := range leafOrder {
		payloadOffs[i] = pCur
		relocs = append(relocs, reloc{vaddr: uint32(rsrc.Len())}) // OffsetToData field
		_ = binary.Write(rsrc, binary.LittleEndian, uint32(pCur))
		_ = binary.Write(rsrc, binary.LittleEndian, uint32(len(lf.data)))
		_ = binary.Write(rsrc, binary.LittleEndian, uint32(0)) // CodePage
		_ = binary.Write(rsrc, binary.LittleEndian, uint32(0)) // Reserved
		pCur += (len(lf.data) + 7) &^ 7
	}
	for rsrc.Len() < payloadStart {
		rsrc.WriteByte(0)
	}
	for _, lf := range leafOrder {
		rsrc.Write(lf.data)
		for rsrc.Len()%8 != 0 {
			rsrc.WriteByte(0)
		}
	}

	rsrcBytes := rsrc.Bytes()

	// Build COFF object file:
	// [FileHeader 20B] [SectionHeader .rsrc 40B] [rsrcBytes] [Relocs 10B*N] [Symbol 18B] [StringTable 4B]
	const (
		coffHdrSize = 20
		secHdrSize  = 40
		relocSize   = 10
		symSize     = 18
	)
	rawDataPtr := uint32(coffHdrSize + secHdrSize)
	relocPtr := rawDataPtr + uint32(len(rsrcBytes))
	symPtr := relocPtr + uint32(len(relocs)*relocSize)

	obj := new(bytes.Buffer)
	// IMAGE_FILE_HEADER
	_ = binary.Write(obj, binary.LittleEndian, uint16(imageFileMachineAMD64))
	_ = binary.Write(obj, binary.LittleEndian, uint16(1))      // NumberOfSections
	_ = binary.Write(obj, binary.LittleEndian, uint32(0))      // TimeDateStamp
	_ = binary.Write(obj, binary.LittleEndian, symPtr)         // PointerToSymbolTable
	_ = binary.Write(obj, binary.LittleEndian, uint32(1))      // NumberOfSymbols
	_ = binary.Write(obj, binary.LittleEndian, uint16(0))      // SizeOfOptionalHeader
	_ = binary.Write(obj, binary.LittleEndian, uint16(0))      // Characteristics

	// IMAGE_SECTION_HEADER (.rsrc)
	var secName [8]byte
	copy(secName[:], ".rsrc")
	obj.Write(secName[:])
	_ = binary.Write(obj, binary.LittleEndian, uint32(0))              // VirtualSize (0 in obj)
	_ = binary.Write(obj, binary.LittleEndian, uint32(0))              // VirtualAddress
	_ = binary.Write(obj, binary.LittleEndian, uint32(len(rsrcBytes))) // SizeOfRawData
	_ = binary.Write(obj, binary.LittleEndian, rawDataPtr)             // PointerToRawData
	_ = binary.Write(obj, binary.LittleEndian, relocPtr)               // PointerToRelocations
	_ = binary.Write(obj, binary.LittleEndian, uint32(0))              // PointerToLinenumbers
	_ = binary.Write(obj, binary.LittleEndian, uint16(len(relocs)))    // NumberOfRelocations
	_ = binary.Write(obj, binary.LittleEndian, uint16(0))              // NumberOfLinenumbers
	_ = binary.Write(obj, binary.LittleEndian, uint32(imageScnCntInitData|imageScnMemRead))

	obj.Write(rsrcBytes)

	for _, rel := range relocs {
		_ = binary.Write(obj, binary.LittleEndian, rel.vaddr)
		_ = binary.Write(obj, binary.LittleEndian, uint32(0)) // SymbolTableIndex = .rsrc
		_ = binary.Write(obj, binary.LittleEndian, uint16(imageRelAMD64Addr32NB))
	}

	// Symbol 0: .rsrc static symbol
	obj.Write(secName[:])
	_ = binary.Write(obj, binary.LittleEndian, uint32(0)) // Value
	_ = binary.Write(obj, binary.LittleEndian, int16(1))  // SectionNumber (1-based)
	_ = binary.Write(obj, binary.LittleEndian, uint16(0)) // Type
	obj.WriteByte(imageSymClassStatic)                    // StorageClass
	obj.WriteByte(0)                                      // NumberOfAuxSymbols

	// Empty COFF string table (4 bytes: total size = 4)
	_ = binary.Write(obj, binary.LittleEndian, uint32(4))

	return obj.Bytes()
}
