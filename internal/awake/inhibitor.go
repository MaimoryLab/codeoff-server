package awake

import "sync"

type acquireFunc func() (func() error, error)

type Inhibitor struct {
	mu      sync.Mutex
	acquire acquireFunc
	release func() error
}

func New() *Inhibitor { return &Inhibitor{acquire: acquire} }

func (i *Inhibitor) Set(enabled bool) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if enabled {
		if i.release != nil {
			return nil
		}
		release, err := i.acquire()
		if err != nil {
			return err
		}
		i.release = release
		return nil
	}
	if i.release == nil {
		return nil
	}
	release := i.release
	i.release = nil
	return release()
}
