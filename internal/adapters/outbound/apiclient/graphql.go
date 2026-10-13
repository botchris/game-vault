package apiclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// GraphQLBody is the JSON body of a GraphQL request.
type GraphQLBody struct {
	// Query is the GraphQL document.
	Query string `json:"query"`

	// OperationName names the operation to run when the document holds several (optional).
	OperationName string `json:"operationName,omitempty"`

	// Variables are the operation's variables (optional).
	Variables map[string]any `json:"variables,omitempty"`
}

// GraphQLError is an answer whose "errors" list is not empty. GraphQL services report most
// failures this way, often with HTTP 200, and name the kind of failure in extensions.code
// ("UNAUTHENTICATED"). When the status was not a 2xx, the error also matches it (IsStatus).
type GraphQLError struct {
	// Code is the first extensions.code the errors carry ("" when none does).
	Code string

	// Message joins the errors' messages.
	Message string

	// Partial reports that the answer also held data, decoded into the caller's value: aliases
	// that failed (an unknown slug) next to aliases that worked.
	Partial bool

	// codes holds every extensions.code, for IsGraphQLCode.
	codes []string

	// status is the answer's *StatusError when it was not a 2xx.
	status error
}

// Error implements error.
func (e *GraphQLError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("GraphQL error %s: %s", e.Code, e.Message)
	}

	return "GraphQL error: " + e.Message
}

// Unwrap returns the *StatusError of an answer that was not a 2xx, so IsStatus matches it.
func (e *GraphQLError) Unwrap() error { return e.status }

// IsGraphQLCode reports whether err is a GraphQLError carrying one of the codes in an error's
// extensions.code.
func IsGraphQLCode(err error, codes ...string) bool {
	var ge *GraphQLError
	if !errors.As(err, &ge) {
		return false
	}

	return slices.ContainsFunc(ge.codes, func(c string) bool { return slices.Contains(codes, c) })
}

// GraphQL posts the query to path, with the request's own headers (a bearer token…), and decodes
// the answer's "data" into out. See Response.GraphQL for what each kind of answer returns.
func (c *Client) GraphQL(ctx context.Context, path string, body GraphQLBody, header http.Header, out any) error {
	res, err := c.Send(ctx, Request{
		Method: http.MethodPost,
		Path:   path,
		Header: header,
		Body:   body,
	})
	if err != nil {
		return err
	}

	return res.GraphQL(out)
}

// GraphQL decodes a GraphQL answer's "data" into out: the way to read a query sent some other way
// (a persisted query by GET). The status is checked first, so no answer is taken for an empty
// one:
//   - a 429 or a 5xx is a *StatusError, whatever the body says (ClassifyError makes it a passing
//     trouble);
//   - an answer with errors is a *GraphQLError (its data, if any, is still decoded, see Partial);
//   - any other status that is not a 2xx is a *StatusError: a gateway's refusal, often JSON
//     without "errors";
//   - a web page is ErrNotJSON.
func (r *Response) GraphQL(out any) error {
	if Transient(r.Status) {
		return r.Err()
	}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message    string `json:"message"`
			Extensions struct {
				Code string `json:"code"`
			} `json:"extensions"`
		} `json:"errors"`
	}

	if err := r.Decode(&envelope); err != nil {
		if !r.OK() {
			return r.Err()
		}

		return err
	}

	hasData := len(envelope.Data) > 0 && string(envelope.Data) != "null"
	if hasData && out != nil {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("unexpected data from %s (the service may have changed): %w", r.endpoint, err)
		}
	}

	if len(envelope.Errors) == 0 {
		return r.Err()
	}

	ge := &GraphQLError{
		Partial: hasData,
		status:  r.Err(),
	}

	messages := make([]string, 0, len(envelope.Errors))
	for _, e := range envelope.Errors {
		messages = append(messages, e.Message)

		if e.Extensions.Code != "" {
			ge.codes = append(ge.codes, e.Extensions.Code)
		}
	}

	ge.Message = strings.Join(messages, "; ")
	if len(ge.codes) > 0 {
		ge.Code = ge.codes[0]
	}

	return ge
}
