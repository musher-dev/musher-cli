package workflow

import "testing"

func TestNopProgress(t *testing.T) {
	t.Parallel()

	var p NopProgress
	// Verify all methods are callable without panic.
	p.Start()
	p.Stop()
	p.StopWithSuccess("done")
	p.StopWithFailure("fail")
	p.UpdateMessage("msg")
}

func TestNopProgressFactory(t *testing.T) {
	t.Parallel()

	var f NopProgressFactory

	progress := f.NewProgress("test")

	if progress == nil {
		t.Fatal("NewProgress returned nil")
	}

	// Verify it's a NopProgress.
	if _, ok := progress.(NopProgress); !ok {
		t.Fatal("NewProgress did not return NopProgress")
	}
}
