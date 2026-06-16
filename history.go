package codex

import pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"

type HistoryOption func(*historyOptions)

type historyOptions struct {
	depth         pb.ChatHistoryDepth
	cursor        string
	limit         uint32
	sortDirection pb.ChatHistorySortDirection
}

func WithHistoryDepth(depth pb.ChatHistoryDepth) HistoryOption {
	return func(opts *historyOptions) {
		opts.depth = depth
	}
}

func WithHistoryCursor(cursor string) HistoryOption {
	return func(opts *historyOptions) {
		opts.cursor = cursor
	}
}

func WithHistoryLimit(limit uint32) HistoryOption {
	return func(opts *historyOptions) {
		opts.limit = limit
	}
}

func WithHistorySortDirection(direction pb.ChatHistorySortDirection) HistoryOption {
	return func(opts *historyOptions) {
		opts.sortDirection = direction
	}
}

func applyHistoryOptions(opts []HistoryOption) historyOptions {
	applied := historyOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&applied)
		}
	}
	return applied
}
