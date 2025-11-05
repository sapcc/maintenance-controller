// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"net/http"
	"testing"
)

func Test_waitParallel(t *testing.T) {
	type args struct {
		waiters []WaitFunc
	}
	tests := []struct {
		name   string
		args   args
		errStr string
	}{
		{
			name: "all succeed",
			args: args{
				waiters: []WaitFunc{
					func() error { return nil },
					func() error { return nil },
				},
			},
			errStr: "",
		},
		{
			name: "one fails",
			args: args{
				waiters: []WaitFunc{
					func() error { return nil },
					func() error { return http.ErrHandlerTimeout },
				},
			},
			errStr: "http: Handler timeout",
		},
		{
			name: "all fail",
			args: args{
				waiters: []WaitFunc{
					func() error { return http.ErrHandlerTimeout },
					func() error { return context.Canceled },
				},
			},
			errStr: "context canceled\nhttp: Handler timeout",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := waitParallel(tt.args.waiters); err != nil && err.Error() != tt.errStr {
				t.Errorf("waitParallel() error = '%v', expected = '%v'", err, tt.errStr)
			}
		})
	}
}
