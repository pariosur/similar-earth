package similarity

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLoadScoresFileMapsLazily(t *testing.T) {
	const width, height, pins = 3, 2, 1
	total := width * height
	buf := make([]byte, 16+total*5+pins*64)
	binary.LittleEndian.PutUint32(buf[0:], width)
	binary.LittleEndian.PutUint32(buf[4:], height)
	binary.LittleEndian.PutUint32(buf[8:], pins)
	for i := 0; i < total; i++ {
		binary.LittleEndian.PutUint32(buf[16+i*4:], math.Float32bits(float32(i)/10))
		buf[16+total*4+i] = byte(i % 2)
	}
	buf[16+total*5] = 0x7f // first ref embedding value
	path := filepath.Join(t.TempDir(), "scores_test.bin")
	if err := os.WriteFile(path, buf, 0600); err != nil {
		t.Fatal(err)
	}

	r, err := loadScoresFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if r.Scores != nil || len(r.RefEmbeddings) != pins || r.RefEmbeddings[0][0] != 127 {
		t.Fatalf("unexpected eager state: scores=%v refs=%v", r.Scores, r.RefEmbeddings)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.EnsureLoaded(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	if len(r.Scores) != total || len(r.BestPinIndex) != total {
		t.Fatalf("lengths: scores=%d best=%d, want %d", len(r.Scores), len(r.BestPinIndex), total)
	}
	for i := 0; i < total; i++ {
		if r.Scores[i] != float32(i)/10 || r.BestPinIndex[i] != byte(i%2) {
			t.Fatalf("pixel %d: score=%v best=%d", i, r.Scores[i], r.BestPinIndex[i])
		}
	}
}
