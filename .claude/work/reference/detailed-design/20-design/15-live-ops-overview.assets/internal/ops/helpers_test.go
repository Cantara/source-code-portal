package ops_test

import "log/slog"

func nil2log() *slog.Logger { return slog.New(slog.DiscardHandler) }
