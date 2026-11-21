package artifact

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

func TestInterceptKeepsHarnessReads(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("x", SmallThreshold+50)
	for _, tool := range []string{"view_file", "list_dir", "grep_search"} {
		result := Intercept(store, "th_test", tool, big)
		if result.WasOffloaded {
			t.Fatalf("%s was offloaded", tool)
		}
		if result.Observation != big {
			t.Fatalf("%s observation changed", tool)
		}
		if result.ArtifactRef != "" {
			t.Fatalf("%s got artifact ref %s", tool, result.ArtifactRef)
		}
	}
}

func TestInterceptOffloadsRunCommandWithoutReadArtifact(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("line of output\n", 200)
	result := Intercept(store, "th_test", "run_command", big)
	if !result.WasOffloaded {
		t.Fatal("run_command should be offloaded")
	}
	if result.ArtifactRef == "" {
		t.Fatal("missing artifact ref")
	}
	if strings.Contains(result.Observation, "read_artifact") {
		t.Fatalf("advertised missing tool: %s", result.Observation)
	}
	if !strings.Contains(result.Observation, "narrower") {
		t.Fatalf("missing steer text: %s", result.Observation)
	}
}

func TestInterceptSmallOutputStaysInline(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	result := Intercept(store, "th_test", "run_command", "ok")
	if result.WasOffloaded || result.Observation != "ok" {
		t.Fatalf("small output = %+v", result)
	}
}

func TestClipStringIsRuneSafe(t *testing.T) {
	s := strings.Repeat("é", SmallThreshold+8)
	got := clipString(s, SmallThreshold)
	if !utf8.ValidString(got) {
		t.Fatal("clipString split a rune")
	}
	if utf8.RuneCountInString(got) != SmallThreshold {
		t.Fatalf("runes = %d", utf8.RuneCountInString(got))
	}
}

func TestInterceptConcurrent(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	viewBody := strings.Repeat("func() {}\n", 300)
	bashBody := strings.Repeat("shell line\n", 300)

	ctxDone := make(chan struct{})
	time.AfterFunc(150*time.Millisecond, func() { close(ctxDone) })

	var failed atomic.Bool
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctxDone:
					return
				default:
				}
				view := Intercept(store, "th_test", "view_file", viewBody)
				if view.WasOffloaded || view.Observation != viewBody {
					failed.Store(true)
					return
				}
				bash := Intercept(store, "th_test", "run_command", bashBody)
				if !bash.WasOffloaded || strings.Contains(bash.Observation, "read_artifact") {
					failed.Store(true)
					return
				}
			}
		}()
	}
	wg.Wait()
	if failed.Load() {
		t.Fatal("concurrent intercept mixed view_file and run_command policy")
	}
}
