package runtime

import (
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestFailedResponseSamplerDeadlineClosesBlockedBody(t *testing.T) {
	body := newBlockingSamplerBody()
	sampler := newFailedResponseSampler("ingress-deadline", &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Body:       body,
	}, "application/json", nil)
	started := time.Now()
	sampler.run()
	if elapsed := time.Since(started); elapsed > FailedResponseSampleDeadline+(250*time.Millisecond) {
		t.Fatalf("sampler exceeded deadline: %s", elapsed)
	}
	if !body.closed() {
		t.Fatal("sampler deadline did not close the blocked body")
	}
	if _, ok := sampler.result.result(); ok {
		t.Fatal("deadline fallback must not expose a provider diagnostic")
	}
}

func TestFailedResponseSamplerLimiterCapsPerIngress(t *testing.T) {
	limiter := &failedResponseSamplerLimiter{}
	for index := 0; index < MaxFailedResponseSamplers; index++ {
		if !limiter.acquire("same-ingress") {
			t.Fatalf("acquire %d rejected before cap", index+1)
		}
	}
	if limiter.acquire("same-ingress") {
		t.Fatal("ninth sampler acquire must be rejected")
	}
	if !limiter.acquire("other-ingress") {
		t.Fatal("limiter must isolate ingress identities")
	}
	limiter.release("same-ingress")
	if !limiter.acquire("same-ingress") {
		t.Fatal("released sampler slot was not reusable")
	}
}

func TestFailedResponseSamplerReleasesSlotAfterBodyCloseCompletes(t *testing.T) {
	limiter := &failedResponseSamplerLimiter{}
	body := newBlockingCloseSamplerBody()
	if !limiter.acquire("same-ingress") {
		t.Fatal("acquire sampler slot")
	}
	sampler := newFailedResponseSampler("same-ingress", &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Body:       body,
	}, "application/json", nil)
	sampler.release = func() { limiter.release("same-ingress") }
	done := make(chan struct{})
	go func() {
		sampler.run()
		close(done)
	}()
	select {
	case <-body.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("sampler did not start closing the body")
	}
	for index := 1; index < MaxFailedResponseSamplers; index++ {
		if !limiter.acquire("same-ingress") {
			t.Fatalf("acquire slot %d while first sampler closes", index+1)
		}
	}
	if limiter.acquire("same-ingress") {
		t.Fatal("sampler whose Close is blocked must retain its limiter slot")
	}
	close(body.allowClose)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sampler did not finish after body close unblocked")
	}
	if !limiter.acquire("same-ingress") {
		t.Fatal("sampler slot was not released after body close completed")
	}
}

type blockingSamplerBody struct {
	closedCh chan struct{}
	once     sync.Once
}

func newBlockingSamplerBody() *blockingSamplerBody {
	return &blockingSamplerBody{closedCh: make(chan struct{})}
}

func (body *blockingSamplerBody) Read([]byte) (int, error) {
	<-body.closedCh
	return 0, io.ErrClosedPipe
}

func (body *blockingSamplerBody) Close() error {
	body.once.Do(func() { close(body.closedCh) })
	return nil
}

func (body *blockingSamplerBody) closed() bool {
	select {
	case <-body.closedCh:
		return true
	default:
		return false
	}
}

type blockingCloseSamplerBody struct {
	closeStarted chan struct{}
	allowClose   chan struct{}
	once         sync.Once
}

func newBlockingCloseSamplerBody() *blockingCloseSamplerBody {
	return &blockingCloseSamplerBody{closeStarted: make(chan struct{}), allowClose: make(chan struct{})}
}

func (body *blockingCloseSamplerBody) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (body *blockingCloseSamplerBody) Close() error {
	body.once.Do(func() { close(body.closeStarted) })
	<-body.allowClose
	return nil
}
