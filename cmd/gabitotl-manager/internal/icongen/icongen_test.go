package icongen

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"testing"
)

func TestICOHeaderAndEntries(t *testing.T) {
	data, err := ICO(Sizes)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint16(data[0:2]); got != 0 {
		t.Fatalf("reserved = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint16(data[2:4]); got != 1 {
		t.Fatalf("type = %d, want 1 (icon)", got)
	}
	n := int(binary.LittleEndian.Uint16(data[4:6]))
	if n != len(Sizes) {
		t.Fatalf("image count = %d, want %d", n, len(Sizes))
	}
	for i, size := range Sizes {
		e := data[6+16*i : 6+16*(i+1)]
		want := byte(size)
		if e[0] != want || e[1] != want {
			t.Errorf("entry %d dimensions = %dx%d, want %d (0 means 256)", i, e[0], e[1], size)
		}
		length := int(binary.LittleEndian.Uint32(e[8:12]))
		offset := int(binary.LittleEndian.Uint32(e[12:16]))
		if offset+length > len(data) {
			t.Fatalf("entry %d overruns the file", i)
		}
		img, err := png.Decode(bytes.NewReader(data[offset : offset+length]))
		if err != nil {
			t.Fatalf("entry %d is not a PNG: %v", i, err)
		}
		if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
			t.Errorf("entry %d image = %dx%d, want %d", i, b.Dx(), b.Dy(), size)
		}
	}
}

func TestRenderShape(t *testing.T) {
	img := Render(64)
	if a := img.NRGBAAt(0, 0).A; a != 0 {
		t.Errorf("corner alpha = %d, want transparent", a)
	}
	if c := img.NRGBAAt(32, 4); c != navy {
		t.Errorf("background = %v, want navy", c)
	}
	if c := img.NRGBAAt(18, 34); c != sky {
		t.Errorf("body = %v, want sky", c)
	}
	if c := img.NRGBAAt(25, 34); c != navy {
		t.Errorf("eye = %v, want navy", c)
	}
	if c := img.NRGBAAt(32, 10); c != amber {
		t.Errorf("antenna dot = %v, want amber", c)
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	a, _ := ICO(Sizes)
	b, _ := ICO(Sizes)
	if !bytes.Equal(a, b) {
		t.Fatal("ICO output differs between runs")
	}
}
