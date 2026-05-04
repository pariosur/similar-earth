package similarity

// DotProductInt8 computes the dot product of two int8 vectors.
// Both slices must have length >= 64. Only the first 64 elements are used.
// The loop is manually unrolled in groups of 8 for throughput.
func DotProductInt8(a, b []int8) int32 {
	_ = a[63] // bounds-check elimination hint
	_ = b[63]

	var s0, s1, s2, s3, s4, s5, s6, s7 int32

	for i := 0; i < 64; i += 8 {
		s0 += int32(a[i]) * int32(b[i])
		s1 += int32(a[i+1]) * int32(b[i+1])
		s2 += int32(a[i+2]) * int32(b[i+2])
		s3 += int32(a[i+3]) * int32(b[i+3])
		s4 += int32(a[i+4]) * int32(b[i+4])
		s5 += int32(a[i+5]) * int32(b[i+5])
		s6 += int32(a[i+6]) * int32(b[i+6])
		s7 += int32(a[i+7]) * int32(b[i+7])
	}

	return (s0 + s1 + s2 + s3) + (s4 + s5 + s6 + s7)
}
