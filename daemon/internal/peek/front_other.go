//go:build !linux && !windows && !darwin

package peek

func NewSampler() (Sampler, string) { return noSampler{}, ModeNone }

type noSampler struct{}

func (noSampler) Front() (Front, error) { return Front{}, nil }
func (noSampler) Close()                {}
