package secretinput

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

// ContentTypeOctetStream is the Core provider-secret-stage body media type.
const ContentTypeOctetStream = "application/octet-stream"

// Code is a typed stage-lifecycle outcome. Values match Core problem codes
// where applicable. Results never embed secret bytes.
type Code string

const (
	CodeOK         Code = ""
	CodeExpired    Code = "stage_expired"
	CodeNotFound   Code = "stage_not_found"
	CodeReserved   Code = "stage_reserved"
	CodeEmpty      Code = "empty_buffer"
	CodeTransport  Code = "transport_error"
	CodeUnexpected Code = "unexpected_response"
	CodeTooLarge   Code = "body_too_large"
)

// Result is a safe async command outcome: stage ID / expiry / typed code only.
// It must never carry secret bytes.
type Result struct {
	StageID   string
	ExpiresAt time.Time
	Code      Code
}

// OK reports a successful stage or discard outcome.
func (r Result) OK() bool {
	return r.Code == CodeOK
}

// String returns a redacted, non-secret description suitable for UI/debug.
func (r Result) String() string {
	if r.Code == CodeOK {
		if r.StageID == "" {
			return "secretinput: ok"
		}
		return "secretinput: staged"
	}
	return "secretinput: " + string(r.Code)
}

// Error returns a stable message that never embeds secret material.
func (r Result) Error() string {
	if r.OK() {
		return ""
	}
	return r.String()
}

// IsStageLoss reports codes that invalidate an owned stage ID.
func IsStageLoss(code Code) bool {
	return code == CodeExpired || code == CodeNotFound
}

// StageHTTPClient abstracts generated Core stage endpoints for tests and
// production adapters. Method names match the generated client.
type StageHTTPClient interface {
	CreateProviderSecretStageWithBodyWithResponse(
		ctx context.Context,
		contentType string,
		body io.Reader,
		reqEditors ...generated.RequestEditorFn,
	) (*generated.CreateProviderSecretStageResponse, error)

	DeleteProviderSecretStageWithResponse(
		ctx context.Context,
		stageId generated.SecretStageID,
		reqEditors ...generated.RequestEditorFn,
	) (*generated.DeleteProviderSecretStageResponse, error)
}

// Stager owns the local provider-secret-stage lifecycle: create from Buffer,
// track owned stage IDs, and idempotent DELETE on cancel/discard.
type Stager struct {
	client    StageHTTPClient
	ownedID   string
	expiresAt time.Time
}

// NewStager returns a Stager bound to the given HTTP client.
func NewStager(client StageHTTPClient) *Stager {
	return &Stager{client: client}
}

// OwnedStageID returns the currently owned unconsumed stage ID, or "".
func (s *Stager) OwnedStageID() string {
	if s == nil {
		return ""
	}
	return s.ownedID
}

// ExpiresAt returns the UTC expiry of the owned stage, or zero.
func (s *Stager) ExpiresAt() time.Time {
	if s == nil {
		return time.Time{}
	}
	return s.expiresAt
}

// NoteLoss clears ownership after a remote loss (expired / not_found / consumed)
// only when stageID matches the currently owned stage. Mismatched IDs leave
// ownership intact so a still-live owned stage is not silently orphaned.
// The previous stage ID must never be reused; the next Stage allocates a new one.
func (s *Stager) NoteLoss(stageID string, code Code) {
	if s == nil || !IsStageLoss(code) {
		return
	}
	if stageID == "" || s.ownedID != stageID {
		return
	}
	s.clearOwnership()
}

// DiscardStageID issues an idempotent DELETE for an arbitrary stage ID.
// If it matches the owned stage and DELETE succeeds or reports stage loss,
// ownership is cleared.
func (s *Stager) DiscardStageID(ctx context.Context, stageID string) Result {
	if s == nil || stageID == "" {
		return Result{Code: CodeOK}
	}
	if s.client == nil {
		return Result{Code: CodeTransport}
	}
	resp, err := s.client.DeleteProviderSecretStageWithResponse(ctx, generated.SecretStageID(stageID))
	if err != nil {
		return Result{Code: CodeTransport}
	}
	if resp == nil {
		return Result{Code: CodeUnexpected}
	}
	status := resp.StatusCode()
	var code Code
	if status == 204 || status == 200 {
		code = CodeOK
	} else if status == 404 {
		code = CodeOK
	} else {
		code = mapDeleteProblem(resp)
		if code == CodeOK && status >= 200 && status < 300 {
			code = CodeOK
		} else if code == CodeOK {
			code = CodeUnexpected
		}
	}
	if code == CodeReserved {
		return Result{Code: CodeReserved}
	}
	if code == CodeOK || IsStageLoss(code) {
		if s.ownedID == stageID {
			s.clearOwnership()
		}
		return Result{Code: CodeOK}
	}
	return Result{Code: code}
}

// ClearOwnership forgets the owned stage without issuing DELETE.
func (s *Stager) ClearOwnership() {
	if s == nil {
		return
	}
	s.clearOwnership()
}

// Stage submits buffer octets to Core, zeros the buffer on success, and
// records ownership. Prior owned stages are DELETE'd first; ownership is
// cleared only on DELETE OK or stage loss. Other DELETE failures (including
// stage_reserved) keep ownership and skip Create.
// The returned Result never contains secret bytes.
func (s *Stager) Stage(ctx context.Context, buf *Buffer) Result {
	if s == nil || s.client == nil {
		return Result{Code: CodeTransport}
	}
	if buf == nil || !buf.HasContent() {
		return Result{Code: CodeEmpty}
	}

	// New paste / restage must not reuse a prior owned ID.
	// Only forget ownership when DELETE succeeded or the remote stage is already gone.
	if s.ownedID != "" {
		del := s.deleteOwned(ctx)
		if del.Code == CodeReserved {
			return del
		}
		if !del.OK() && !IsStageLoss(del.Code) {
			return del
		}
		s.clearOwnership()
	}

	reqBody := make([]byte, buf.Len())
	n := buf.CopyBytes(reqBody)
	reqBody = reqBody[:n]
	defer func() {
		for i := range reqBody {
			reqBody[i] = 0
		}
	}()

	resp, err := s.client.CreateProviderSecretStageWithBodyWithResponse(
		ctx,
		ContentTypeOctetStream,
		bytes.NewReader(reqBody),
	)
	if err != nil {
		return Result{Code: CodeTransport}
	}
	if resp == nil {
		return Result{Code: CodeUnexpected}
	}

	if resp.StatusCode() == 201 && resp.JSON201 != nil {
		id := string(resp.JSON201.StageId)
		exp := resp.JSON201.ExpiresAt.UTC()
		if id == "" {
			return Result{Code: CodeUnexpected}
		}
		s.ownedID = id
		s.expiresAt = exp
		buf.Zero()
		return Result{StageID: id, ExpiresAt: exp, Code: CodeOK}
	}

	code := mapCreateProblem(resp)
	if IsStageLoss(code) {
		s.clearOwnership()
	}
	return Result{Code: code}
}

// Discard issues an idempotent DELETE for the owned unconsumed stage and zeros
// buf. Missing/expired/consumed stages are treated as success (204).
func (s *Stager) Discard(ctx context.Context, buf *Buffer) Result {
	if s == nil {
		if buf != nil {
			buf.Zero()
		}
		return Result{Code: CodeOK}
	}
	res := Result{Code: CodeOK}
	if s.ownedID != "" && s.client != nil {
		res = s.deleteOwned(ctx)
		if res.Code == CodeReserved {
			// Keep ownership; still zero local buffers so cancel does not retain secrets.
			if buf != nil {
				buf.Zero()
			}
			return res
		}
		if res.OK() || IsStageLoss(res.Code) {
			s.clearOwnership()
			res = Result{Code: CodeOK}
		} else if res.Code != CodeOK {
			if buf != nil {
				buf.Zero()
			}
			return res
		}
	}
	if buf != nil {
		buf.Zero()
	}
	return res
}

func (s *Stager) deleteOwned(ctx context.Context) Result {
	id := s.ownedID
	if id == "" || s.client == nil {
		return Result{Code: CodeOK}
	}
	resp, err := s.client.DeleteProviderSecretStageWithResponse(ctx, generated.SecretStageID(id))
	if err != nil {
		return Result{Code: CodeTransport}
	}
	if resp == nil {
		return Result{Code: CodeUnexpected}
	}
	status := resp.StatusCode()
	// 204 and other non-problem successes are idempotent OK.
	if status == 204 || status == 200 {
		return Result{Code: CodeOK}
	}
	code := mapDeleteProblem(resp)
	if code == CodeOK && status >= 200 && status < 300 {
		return Result{Code: CodeOK}
	}
	if code == CodeOK {
		// Non-2xx without a recognizable problem: treat missing as idempotent OK
		// only when status is 404; otherwise unexpected.
		if status == 404 {
			return Result{Code: CodeOK}
		}
		return Result{Code: CodeUnexpected}
	}
	return Result{Code: code}
}

func (s *Stager) clearOwnership() {
	s.ownedID = ""
	s.expiresAt = time.Time{}
}

func mapCreateProblem(resp *generated.CreateProviderSecretStageResponse) Code {
	if resp == nil {
		return CodeUnexpected
	}
	switch {
	case resp.ApplicationproblemJSON413 != nil:
		return codeFromProblem(resp.ApplicationproblemJSON413.Code, CodeTooLarge)
	case resp.ApplicationproblemJSON422 != nil:
		return codeFromProblem(resp.ApplicationproblemJSON422.Code, CodeUnexpected)
	case resp.ApplicationproblemJSONDefault != nil:
		return codeFromProblem(resp.ApplicationproblemJSONDefault.Code, CodeUnexpected)
	default:
		return CodeUnexpected
	}
}

func mapDeleteProblem(resp *generated.DeleteProviderSecretStageResponse) Code {
	if resp == nil {
		return CodeUnexpected
	}
	switch {
	case resp.ApplicationproblemJSON409 != nil:
		return codeFromProblem(resp.ApplicationproblemJSON409.Code, CodeReserved)
	case resp.ApplicationproblemJSONDefault != nil:
		return codeFromProblem(resp.ApplicationproblemJSONDefault.Code, CodeUnexpected)
	default:
		return CodeOK
	}
}

func codeFromProblem(raw string, fallback Code) Code {
	switch Code(raw) {
	case CodeExpired, CodeNotFound, CodeReserved, CodeTooLarge:
		return Code(raw)
	case "":
		return fallback
	default:
		return fallback
	}
}
