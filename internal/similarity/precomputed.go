package similarity

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"github.com/google/uuid"
	"github.com/pariosur/tierraai/internal/mmapfile"
)

// PrecomputedStore holds pre-loaded similarity results for known layers.
type PrecomputedStore struct {
	mu      sync.RWMutex
	results map[string]*QueryResult // layer ID → result
}

// NewPrecomputedStore creates an empty store.
func NewPrecomputedStore() *PrecomputedStore {
	return &PrecomputedStore{
		results: make(map[string]*QueryResult),
	}
}

// LoadDir loads all scores_*.bin files from a directory.
func (s *PrecomputedStore) LoadDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read scores dir: %w", err)
	}

	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "scores_") || !strings.HasSuffix(name, ".bin") {
			continue
		}
		layerID := strings.TrimPrefix(strings.TrimSuffix(name, ".bin"), "scores_")
		path := filepath.Join(dir, name)

		result, err := loadScoresFile(path)
		if err != nil {
			log.Printf("WARNING: failed to load %s: %v", path, err)
			continue
		}

		s.mu.Lock()
		s.results[layerID] = result
		s.mu.Unlock()

		log.Printf("Loaded pre-computed scores for %s (%dx%d, %d refs)",
			layerID, result.Width, result.Height, len(result.RefEmbeddings))
	}

	return nil
}

// Get returns the pre-computed result for a crop, or nil if not found.
func (s *PrecomputedStore) Get(layerID string) *QueryResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.results[layerID]
}

// LayerIDs returns all loaded layer IDs.
func (s *PrecomputedStore) LayerIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.results))
	for id := range s.results {
		ids = append(ids, id)
	}
	return ids
}

func loadScoresFile(path string) (*QueryResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Read header: width, height, pin_count, reserved (4 uint32s)
	var header [4]uint32
	if err := binary.Read(f, binary.LittleEndian, &header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	width := int(header[0])
	height := int(header[1])
	pinCount := int(header[2])
	total := width * height

	// Memory-map the scores section instead of reading into RAM.
	// File layout after 16-byte header:
	//   scores:    total * 4 bytes (float32)
	//   best_pin:  total * 1 byte  (uint8)
	//   ref_emb:   pinCount * 64 bytes (int8)

	// For now, just read scores lazily — only read ref embeddings eagerly
	// since they're small (pinCount * 64 bytes).

	// Seek past scores and best_pin to read ref embeddings
	scoresSize := int64(total) * 4
	bestPinSize := int64(total)
	refOffset := 16 + scoresSize + bestPinSize

	if _, err := f.Seek(refOffset, 0); err != nil {
		return nil, fmt.Errorf("seek to ref embeddings: %w", err)
	}

	refData := make([]byte, pinCount*64)
	if _, err := io.ReadFull(f, refData); err != nil {
		return nil, fmt.Errorf("read ref embeddings: %w", err)
	}
	refs := make([][]int8, pinCount)
	for i := 0; i < pinCount; i++ {
		ref := make([]int8, 64)
		for j := 0; j < 64; j++ {
			ref[j] = int8(refData[i*64+j])
		}
		refs[i] = ref
	}

	return &QueryResult{
		QueryID:       uuid.Nil,
		Scores:        nil, // loaded on demand
		BestPinIndex:  nil,
		Width:         width,
		Height:        height,
		RefEmbeddings: refs,
		scoresPath:    path,
	}, nil
}

// EnsureLoaded maps the scores file on first use. Scores and BestPinIndex
// point into the read-only mapping, so they sit in the page cache: the kernel
// only reads the parts a request touches and can drop them under memory
// pressure, rather than each layer pinning ~700 MB of heap for good.
func (r *QueryResult) EnsureLoaded() error {
	r.loadMu.Lock()
	defer r.loadMu.Unlock()
	if r.Scores != nil {
		return nil
	}
	if r.scoresPath == "" {
		return fmt.Errorf("no scores path set")
	}
	if !mmapfile.LittleEndian() {
		return fmt.Errorf("scores files are little-endian; host is not")
	}

	file, err := mmapfile.Open(r.scoresPath)
	if err != nil {
		return err
	}
	buf := file.Bytes()

	// Layout: 16-byte header, float32 scores, uint8 best_pin, ref embeddings.
	total := r.Width * r.Height
	scoresOff, bestPinOff := 16, 16+total*4
	if total == 0 || len(buf) < bestPinOff+total {
		file.Close()
		return fmt.Errorf("scores file %s truncated: %d bytes, want at least %d", r.scoresPath, len(buf), bestPinOff+total)
	}

	// The mapping is page-aligned and the header is 16 bytes, so the float32
	// section is 4-byte aligned and can be viewed in place. The mapping is
	// never unmapped: layers stay addressable for the life of the process.
	r.Scores = unsafe.Slice((*float32)(unsafe.Pointer(&buf[scoresOff])), total)
	r.BestPinIndex = buf[bestPinOff : bestPinOff+total : bestPinOff+total]
	return nil
}
