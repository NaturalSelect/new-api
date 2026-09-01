package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestMatchContentPolicyWarning(t *testing.T) {
	testCases := []struct {
		name        string
		err         *types.NewAPIError
		wantMatched bool
	}{
		{
			name:        "nil error does not match",
			err:         nil,
			wantMatched: false,
		},
		{
			name:        "openai invalid prompt content policy message matches",
			err:         types.NewErrorWithStatusCode(errors.New("Invalid prompt: your prompt was flagged as potentially violating our usage policy. If you believe this to be in error, please submit your prompt for review"), types.ErrorCodeInvalidRequest, 400),
			wantMatched: true,
		},
		{
			name:        "azure/openai safety system rejection matches case-insensitively",
			err:         types.NewErrorWithStatusCode(errors.New("YOUR REQUEST WAS REJECTED BY THE SAFETY SYSTEM. If you believe this is an error, contact us at abuse@example.com"), types.ErrorCodeInvalidRequest, 400),
			wantMatched: true,
		},
		{
			name:        "gemini prompt_blocked error code matches regardless of message text",
			err:         types.NewErrorWithStatusCode(errors.New("request could not be completed"), types.ErrorCodePromptBlocked, 400),
			wantMatched: true,
		},
		{
			name:        "ordinary model-not-found error does not match",
			err:         types.NewErrorWithStatusCode(errors.New("model not found"), types.ErrorCodeModelNotFound, 404),
			wantMatched: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			matched, keyword := MatchContentPolicyWarning(tc.err)
			require.Equal(t, tc.wantMatched, matched)
			if !tc.wantMatched {
				require.Empty(t, keyword)
				return
			}
			require.NotEmpty(t, keyword)
			// The gemini case matches purely via error code, not text, so the keyword
			// (the error code string) is not expected to appear in the message.
			if tc.err.GetErrorCode() != types.ErrorCodePromptBlocked {
				require.Contains(t, strings.ToLower(tc.err.Error()), keyword)
			}
		})
	}
}
