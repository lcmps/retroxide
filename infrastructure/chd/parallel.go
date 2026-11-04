package chd

import (
	"runtime"
	"sync"
)

// maxDecoders bounds the goroutines decompressing hunks for one stream: enough to use a small
// board's cores, without taking every core of a large server for one disc.
const maxDecoders = 4

// hunkStream decompresses a range of hunks in parallel and returns them in order.
type hunkStream struct {
	ahead   chan struct{}
	results []chan hunkResult
	cursor  int
	done    chan struct{}
	once    sync.Once
	wg      sync.WaitGroup
}

type hunkResult struct {
	data []byte
	err  error
}

// decodeHunks starts decompressing hunks first through last (inclusive). Each worker takes the
// next hunk in turn, and at most a few hunks per worker wait to be consumed.
func (reader *Reader) decodeHunks(first, last int) *hunkStream {
	count := last - first + 1
	workers := min(runtime.GOMAXPROCS(0), maxDecoders, count)
	stream := &hunkStream{results: make([]chan hunkResult, count), done: make(chan struct{})}
	for index := range stream.results {
		stream.results[index] = make(chan hunkResult, 1)
	}
	// ahead admits a hunk once the one window hunks before it has been consumed.
	ahead := make(chan struct{}, workers*2)
	var next int
	var nextMu sync.Mutex
	for range workers {
		stream.wg.Go(func() {
			for {
				// Check for a stop first: once a stream stops, admission tokens are drained
				// freely, and a worker must not take that as leave to keep decoding.
				select {
				case <-stream.done:
					return
				default:
				}
				select {
				case ahead <- struct{}{}:
				case <-stream.done:
					return
				}
				nextMu.Lock()
				index := next
				next++
				nextMu.Unlock()
				if index >= count {
					return
				}
				data := make([]byte, reader.header.HunkBytes)
				err := reader.ReadHunk(uint32(first+index), data)
				stream.results[index] <- hunkResult{data: data, err: err}
			}
		})
	}
	stream.ahead = ahead
	return stream
}

// next returns the next hunk in order.
func (stream *hunkStream) next() ([]byte, error) {
	if stream.cursor >= len(stream.results) {
		return nil, ErrCorrupt
	}
	result := <-stream.results[stream.cursor]
	stream.cursor++
	<-stream.ahead
	return result.data, result.err
}

// stop ends the workers; hunks not yet consumed are discarded.
func (stream *hunkStream) stop() {
	stream.once.Do(func() { close(stream.done) })
	// Free workers blocked on admission or on a full result slot, then wait for them.
	go func() {
		for range stream.ahead {
		}
	}()
	stream.wg.Wait()
	close(stream.ahead)
}
