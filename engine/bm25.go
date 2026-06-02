package engine

import "math"

const (
	k1 = 1.2
	b  = 0.75
)

func idf(nTotal, nDocWithTerm uint64) float64 {
	N := float64(nTotal)
	n := float64(nDocWithTerm)
	return math.Log((N-n+0.5)/(n+0.5) + 1)
}

func bm25Score(tf int, docLen uint64, avgdl float64, idfVal float64) float64 {
	TF := float64(tf)
	dl := float64(docLen)
	numerator := TF * (k1 + 1)
	denominator := TF + k1*(1-b+b*dl/avgdl)
	return idfVal * numerator / denominator
}
