// Package tokenize turns text into hashed feature ids.
//
// It must match train/common.py exactly. The golden test proves that.
package tokenize

const (
	// Buckets is the size of the hashed vocabulary (2^18).
	Buckets = 1 << 18
	// MaxWords caps the words kept from one text.
	MaxWords = 64
	// MaxFeatures is the most features one text can produce.
	MaxFeatures = 2*MaxWords - 1

	fnvOffset = 2166136261
	fnvPrime  = 16777619
)

func fnv1a(h uint32, b byte) uint32 {
	h ^= uint32(b)
	return h * fnvPrime
}

// Features returns hashed unigram buckets followed by hashed bigram buckets.
// Only ASCII letters and digits make words. Every other byte splits words.
func Features(text string) []int64 {
	type span struct{ start, end int }
	buf := make([]byte, 0, len(text))
	var spans []span
	start := -1
	for i := 0; i < len(text); i++ {
		b := text[i]
		if b >= 'A' && b <= 'Z' {
			b += 32
		}
		if (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') {
			if start < 0 {
				start = len(buf)
			}
			buf = append(buf, b)
			continue
		}
		if start >= 0 {
			spans = append(spans, span{start, len(buf)})
			start = -1
		}
	}
	if start >= 0 {
		spans = append(spans, span{start, len(buf)})
	}
	if len(spans) > MaxWords {
		spans = spans[:MaxWords]
	}

	ids := make([]int64, 0, 2*len(spans))
	for _, s := range spans {
		h := uint32(fnvOffset)
		for _, b := range buf[s.start:s.end] {
			h = fnv1a(h, b)
		}
		ids = append(ids, int64(h%Buckets))
	}
	for i := 0; i+1 < len(spans); i++ {
		h := uint32(fnvOffset)
		for _, b := range buf[spans[i].start:spans[i].end] {
			h = fnv1a(h, b)
		}
		h = fnv1a(h, ' ')
		for _, b := range buf[spans[i+1].start:spans[i+1].end] {
			h = fnv1a(h, b)
		}
		ids = append(ids, int64(h%Buckets))
	}
	return ids
}
