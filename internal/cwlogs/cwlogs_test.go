package cwlogs

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

type fakeAPI struct {
	events []types.FilteredLogEvent
	calls  []int64
	onCall func(n int)
}

func (f *fakeAPI) FilterLogEvents(_ context.Context, in *cloudwatchlogs.FilterLogEventsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.FilterLogEventsOutput, error) {
	f.calls = append(f.calls, aws.ToInt64(in.StartTime))
	if f.onCall != nil {
		f.onCall(len(f.calls))
	}
	var out []types.FilteredLogEvent
	for _, ev := range f.events {
		if aws.ToInt64(ev.Timestamp) >= aws.ToInt64(in.StartTime) {
			out = append(out, ev)
		}
	}
	return &cloudwatchlogs.FilterLogEventsOutput{Events: out}, nil
}

func event(id string, ts int64, msg string) types.FilteredLogEvent {
	return types.FilteredLogEvent{EventId: aws.String(id), Timestamp: aws.Int64(ts), Message: aws.String(msg + "\n"), LogStreamName: aws.String("s")}
}

func TestTailFollowDeduplicates(t *testing.T) {
	PollInterval = time.Millisecond
	api := &fakeAPI{events: []types.FilteredLogEvent{event("a", 1000, "one"), event("b", 2000, "two")}}
	ctx, cancel := context.WithCancel(context.Background())
	api.onCall = func(n int) {
		switch n {
		case 2:
			api.events = append(api.events, event("c", 2000, "three"), event("d", 3000, "four"))
		case 4:
			cancel()
		}
	}

	var got []string
	err := Tail(ctx, api, "g", time.UnixMilli(0), true, func(s string) { got = append(got, s) })
	if err != context.Canceled {
		t.Fatalf("err = %v", err)
	}
	want := []string{"one", "two", "three", "four"}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i, w := range want {
		if suffix := " s " + w; got[i][len(got[i])-len(suffix):] != suffix {
			t.Errorf("line %d = %q, want suffix %q", i, got[i], suffix)
		}
	}
	if api.calls[1] != 2000 || api.calls[2] != 3000 {
		t.Fatalf("start times = %v", api.calls)
	}
}

func TestTailNoFollowReturns(t *testing.T) {
	api := &fakeAPI{events: []types.FilteredLogEvent{event("a", 1000, "one")}}
	var n int
	if err := Tail(context.Background(), api, "g", time.UnixMilli(0), false, func(string) { n++ }); err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(api.calls) != 1 {
		t.Fatalf("n=%d calls=%v", n, api.calls)
	}
}
