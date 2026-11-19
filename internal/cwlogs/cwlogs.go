// Package cwlogs reads and follows CloudWatch Logs groups.
package cwlogs

import (
	"context"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

// PollInterval is how often Tail polls for new events when following.
var PollInterval = 2 * time.Second

// FilterAPI is the subset of the CloudWatch Logs client used by Tail.
type FilterAPI interface {
	FilterLogEvents(ctx context.Context, in *cloudwatchlogs.FilterLogEventsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.FilterLogEventsOutput, error)
}

// New returns a CloudWatch Logs client for cfg.
func New(cfg aws.Config) FilterAPI { return cloudwatchlogs.NewFromConfig(cfg) }

// Tail passes events of group newer than since to sink, formatted like
// `aws logs tail`. When follow is set it keeps polling until ctx is cancelled.
func Tail(ctx context.Context, api FilterAPI, group string, since time.Time, follow bool, sink func(string)) error {
	start := since.UnixMilli()
	// seen holds IDs of events at the current start timestamp, since the next
	// poll re-reads that millisecond.
	seen := map[string]bool{}

	for {
		latest := start
		latestIDs := map[string]bool{}
		in := &cloudwatchlogs.FilterLogEventsInput{LogGroupName: aws.String(group), StartTime: aws.Int64(start)}
		p := cloudwatchlogs.NewFilterLogEventsPaginator(api, in)
		for p.HasMorePages() {
			page, err := p.NextPage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return err
			}
			for _, ev := range page.Events {
				id := aws.ToString(ev.EventId)
				if seen[id] {
					continue
				}
				sink(Format(ev))
				ts := aws.ToInt64(ev.Timestamp)
				switch {
				case ts > latest:
					latest = ts
					latestIDs = map[string]bool{id: true}
				case ts == latest:
					latestIDs[id] = true
				}
			}
		}
		if !follow {
			return nil
		}
		if latest > start {
			seen = latestIDs
		} else {
			for id := range latestIDs {
				seen[id] = true
			}
		}
		start = latest

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(PollInterval):
		}
	}
}

// Format renders an event as "<timestamp> <stream> <message>".
func Format(ev types.FilteredLogEvent) string {
	ts := time.UnixMilli(aws.ToInt64(ev.Timestamp)).UTC().Format("2006-01-02T15:04:05.000Z")
	msg := strings.TrimRight(aws.ToString(ev.Message), "\r\n")
	return ts + " " + aws.ToString(ev.LogStreamName) + " " + msg
}
