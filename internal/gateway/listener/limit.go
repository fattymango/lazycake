package listener

import (
	"context"
	"io"

	"golang.org/x/time/rate"
)

// NewBandwidthLimiter builds the limiter that caps what a gateway moves: one
// shared by every forwarded connection, counting both directions together.
// mbps <= 0 means no limit (nil).
func NewBandwidthLimiter(mbps int) *rate.Limiter {
	if mbps <= 0 {
		return nil
	}
	perSecond := mbps * 1_000_000 / 8
	burst := perSecond / 10 // a tenth of a second of traffic
	if burst < 64*1024 {
		burst = 64 * 1024
	}
	return rate.NewLimiter(rate.Limit(perSecond), burst)
}

// limitedWriter slows writes to the limiter's rate. A nil limiter passes everything straight through.
type limitedWriter struct {
	ctx context.Context
	w   io.Writer
	lim *rate.Limiter
}

func (l limitedWriter) Write(p []byte) (int, error) {
	if l.lim == nil {
		return l.w.Write(p)
	}
	written := 0
	for written < len(p) {
		chunk := p[written:]
		if max := l.lim.Burst(); len(chunk) > max {
			chunk = chunk[:max]
		}
		if err := l.lim.WaitN(l.ctx, len(chunk)); err != nil {
			return written, err
		}
		n, err := l.w.Write(chunk)
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}
