package bw

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func assertWidth(t *testing.T, text string, width int) {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if ansi.StringWidth(line) > width {
			t.Fatalf("width %d exceeded: %q", width, line)
		}
	}
	if strings.ContainsAny(text, "\x1b\r\x00") {
		t.Fatalf("terminal control in output: %q", text)
	}
}

func TestReadableOutputWidths(t *testing.T) {
	for _, width := range []int{24, 40, 60, 80, 100, 140} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			var b bytes.Buffer
			w := &humanWriter{Writer: &b, width: width}
			rows := TaskList{
				{View: View{Task: Task{ID: "customer-identity", Title: "Customer accounts in control", Priority: 10}, State: "ready"}},
				{View: View{Task: Task{ID: "event-coverage", Title: "Web client and dev console as telemetry producers"}, State: "waiting"}, BlockedBy: []Blocker{{ID: "customer-api", State: "claimed", Holder: "worker with a long name", Idle: "2h", Stale: true}, {ID: "allowance-anniversary", State: "ready"}, {ID: "web-telemetry", State: "ready"}}},
			}
			printTasks(w, rows)
			printViews(w, []View{{Task: Task{ID: "unicode", Title: "日本語のタスク 👩🏽‍💻 café\twith\nnewlines\x1b[31m"}, State: "claimed", Holder: "worker\tname", Age: "1h", Idle: "5m"}})
			printStats(w, Statistics{Run: RunMetrics{Started: new(time.Now()), Total: 2}, Workers: []WorkerMetrics{{Worker: "worker with a long name", Current: []View{{Task: Task{ID: "event-coverage"}, Age: "1h"}}}}, Tasks: []TaskMetrics{{ID: "event-coverage", Holders: []ClaimPeriod{{Worker: "first worker"}, {Worker: "second worker"}}}}})
			printBoard(w, []Event{{Actor: "worker", Event: "noted", Task: "event-coverage", Note: strings.Repeat("handoff details ", 20)}})
			fmt.Fprintln(w, strings.Repeat("unbroken", 30))
			w.flush()
			assertWidth(t, b.String(), width)
			// The screenshot's long blocker information survives wrapping. Compare
			// without whitespace so cell continuation boundaries do not matter.
			compact := strings.Join(strings.Fields(b.String()), "")
			for _, want := range []string{"customer-identity", "Customeraccountsincontrol", "allowance-anniversary", "workerwithalongname", "stale", "日本語のタスク", "handoffdetails", strings.Repeat("unbroken", 30)} {
				if !strings.Contains(compact, want) {
					t.Fatalf("lost %q:\n%s", want, b.String())
				}
			}
			if width == 100 {
				t.Logf("Sample output:\n%s", b.String())
			}
		})
	}
}

func TestHumanWriterPartialWrites(t *testing.T) {
	var b bytes.Buffer
	w := &humanWriter{Writer: &b, width: 40}
	fmt.Fprint(w, "Note: ")
	fmt.Fprint(w, strings.Repeat("long text ", 10))
	fmt.Fprint(w, "\nlast partial line")
	w.flush()
	assertWidth(t, b.String(), 40)
	if !strings.HasPrefix(b.String(), "Note: ") || !strings.HasSuffix(b.String(), "last partial line\n") {
		t.Fatal(b.String())
	}
}

func TestJSONKeepsOriginalText(t *testing.T) {
	title := "Title\twith\nnewlines\x1b[31m" + strings.Repeat("very long title ", 20)
	catalog, _ := marshal(Catalog{Tasks: []Task{{ID: "a", Title: title}}})
	root := fixture(t, string(catalog))
	for _, command := range []string{"tasks", "queue", "q"} {
		if command == "tasks" {
			rows := readData[TaskList](t, invoke(t, root, 0, command))
			if rows[0].Title != title {
				t.Fatal(rows[0].Title)
			}
		} else if rows := views(t, invoke(t, root, 0, command, "--all")); rows[0].Title != title {
			t.Fatal(rows[0].Title)
		}
		assertWidth(t, textCommand(t, root, command), 100)
	}
}
