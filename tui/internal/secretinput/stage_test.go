package secretinput_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/secretinput"
)

// plantedToken builds a distinctive probe at runtime (not a production credential).
func plantedToken() []byte {
	parts := [][]byte{
		{0x70, 0x6c, 0x61, 0x6e, 0x74, 0x65, 0x64}, // planted
		{'-'},
		{0x73, 0x74, 0x61, 0x67, 0x65}, // stage
		{'-'},
		{0x74, 0x6f, 0x6b, 0x65, 0x6e}, // token
		{'-'},
		{0x41, 0x41, 0x42, 0x42, 0x43, 0x43, 0x44, 0x44},
	}
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

type fakeStageHTTP struct {
	createFn func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error)
	deleteFn func(ctx context.Context, stageID generated.SecretStageID) (*generated.DeleteProviderSecretStageResponse, error)

	lastCreateBody  []byte
	lastContentType string
	createCalls     int
	deleteCalls     int
	deletedIDs      []string
}

func (f *fakeStageHTTP) CreateProviderSecretStageWithBodyWithResponse(
	ctx context.Context,
	contentType string,
	body io.Reader,
	_ ...generated.RequestEditorFn,
) (*generated.CreateProviderSecretStageResponse, error) {
	f.createCalls++
	f.lastContentType = contentType
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	f.lastCreateBody = append([]byte(nil), raw...)
	if f.createFn == nil {
		return nil, errors.New("createFn not set")
	}
	return f.createFn(ctx, contentType, raw)
}

func (f *fakeStageHTTP) DeleteProviderSecretStageWithResponse(
	ctx context.Context,
	stageID generated.SecretStageID,
	_ ...generated.RequestEditorFn,
) (*generated.DeleteProviderSecretStageResponse, error) {
	f.deleteCalls++
	f.deletedIDs = append(f.deletedIDs, string(stageID))
	if f.deleteFn == nil {
		return buildDelete(204, ""), nil
	}
	return f.deleteFn(ctx, stageID)
}

func buildCreate(status int, stageID string, exp time.Time, problemCode string) *generated.CreateProviderSecretStageResponse {
	resp := &generated.CreateProviderSecretStageResponse{
		HTTPResponse: &http.Response{StatusCode: status},
	}
	if status == 201 {
		resp.JSON201 = &generated.ProviderSecretStage{
			StageId:   generated.SecretStageID(stageID),
			ExpiresAt: exp,
		}
		return resp
	}
	prob := &generated.Problem{Code: problemCode, Status: status, Title: problemCode}
	switch status {
	case 413:
		resp.ApplicationproblemJSON413 = prob
	case 422:
		resp.ApplicationproblemJSON422 = prob
	default:
		resp.ApplicationproblemJSONDefault = prob
	}
	return resp
}

func buildDelete(status int, problemCode string) *generated.DeleteProviderSecretStageResponse {
	resp := &generated.DeleteProviderSecretStageResponse{
		HTTPResponse: &http.Response{StatusCode: status},
	}
	if problemCode == "" {
		return resp
	}
	prob := &generated.Problem{Code: problemCode, Status: status, Title: problemCode}
	if status == 409 {
		resp.ApplicationproblemJSON409 = prob
	} else {
		resp.ApplicationproblemJSONDefault = prob
	}
	return resp
}

func assertNoSecretLeak(t *testing.T, secret []byte, values ...string) {
	t.Helper()
	needle := string(secret)
	for _, v := range values {
		if needle != "" && strings.Contains(v, needle) {
			t.Fatalf("secret bytes leaked into capture")
		}
	}
}

func TestStageExpiryMapsToTypedCodeWithoutSecret(t *testing.T) {
	secret := plantedToken()
	fake := &fakeStageHTTP{
		createFn: func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
			return buildCreate(410, "", time.Time{}, "stage_expired"), nil
		},
	}
	stager := secretinput.NewStager(fake)
	buf := secretinput.New()
	if err := buf.PasteBytes(secret); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}

	res := stager.Stage(context.Background(), buf)
	if res.Code != secretinput.CodeExpired {
		t.Fatalf("Code = %q, want %q", res.Code, secretinput.CodeExpired)
	}
	if res.StageID != "" {
		t.Fatalf("StageID must be empty on failure, got %q", res.StageID)
	}
	if stager.OwnedStageID() != "" {
		t.Fatalf("must not own a stage after expiry create failure")
	}
	assertNoSecretLeak(t, secret, res.String(), res.Error())
	if !buf.HasUnstaged() {
		t.Fatal("buffer should remain for retry after failed stage")
	}
}

func TestStageNotFoundMapsToTypedCode(t *testing.T) {
	secret := plantedToken()
	fake := &fakeStageHTTP{
		createFn: func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
			return buildCreate(404, "", time.Time{}, "stage_not_found"), nil
		},
	}
	stager := secretinput.NewStager(fake)
	buf := secretinput.New()
	if err := buf.PasteBytes(secret); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}

	res := stager.Stage(context.Background(), buf)
	if res.Code != secretinput.CodeNotFound {
		t.Fatalf("Code = %q, want %q", res.Code, secretinput.CodeNotFound)
	}
	assertNoSecretLeak(t, secret, res.String(), res.Error())
}

func TestDiscardReservedMapsToTypedCode(t *testing.T) {
	secret := plantedToken()
	exp := time.Now().UTC().Add(5 * time.Minute)
	fake := &fakeStageHTTP{
		createFn: func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
			return buildCreate(201, "stg_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1", exp, ""), nil
		},
		deleteFn: func(ctx context.Context, stageID generated.SecretStageID) (*generated.DeleteProviderSecretStageResponse, error) {
			return buildDelete(409, "stage_reserved"), nil
		},
	}
	stager := secretinput.NewStager(fake)
	buf := secretinput.New()
	if err := buf.PasteBytes(secret); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	if res := stager.Stage(context.Background(), buf); !res.OK() {
		t.Fatalf("Stage failed: %v", res)
	}
	// Re-paste so Discard has local secret bytes to clear under reservation.
	if err := buf.PasteBytes(secret); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}

	res := stager.Discard(context.Background(), buf)
	if res.Code != secretinput.CodeReserved {
		t.Fatalf("Code = %q, want %q", res.Code, secretinput.CodeReserved)
	}
	assertNoSecretLeak(t, secret, res.String(), res.Error())
	// Ownership retained so caller can retry discard / observe reservation.
	if stager.OwnedStageID() == "" {
		t.Fatal("owned stage should remain when DELETE is reserved")
	}
	if buf.HasContent() || buf.HasUnstaged() {
		t.Fatal("Discard must zero buffer even when DELETE is reserved")
	}
}

func TestDiscardIdempotentDeleteAndZerosBuffer(t *testing.T) {
	secret := plantedToken()
	exp := time.Now().UTC().Add(5 * time.Minute)
	stageID := "stg_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb2"
	fake := &fakeStageHTTP{
		createFn: func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
			return buildCreate(201, stageID, exp, ""), nil
		},
		deleteFn: func(ctx context.Context, id generated.SecretStageID) (*generated.DeleteProviderSecretStageResponse, error) {
			// Missing/expired/consumed → 204 idempotent.
			return buildDelete(204, ""), nil
		},
	}
	stager := secretinput.NewStager(fake)
	buf := secretinput.New()
	if err := buf.PasteBytes(secret); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	if res := stager.Stage(context.Background(), buf); !res.OK() {
		t.Fatalf("Stage: %v", res)
	}

	// Re-paste so Discard has buffer content to zero.
	if err := buf.PasteBytes(secret); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	res := stager.Discard(context.Background(), buf)
	if !res.OK() || res.Code != secretinput.CodeOK {
		t.Fatalf("Discard = %+v", res)
	}
	if fake.deleteCalls < 1 || fake.deletedIDs[0] != stageID {
		t.Fatalf("DELETE calls=%d ids=%v want %q", fake.deleteCalls, fake.deletedIDs, stageID)
	}
	if stager.OwnedStageID() != "" {
		t.Fatal("ownership must clear after Discard")
	}
	if buf.HasContent() || buf.HasUnstaged() {
		t.Fatal("Discard must zero buffer")
	}
	for i := 0; i < buf.Cap(); i++ {
		if buf.ByteAt(i) != 0 {
			t.Fatalf("backing[%d] not zeroed", i)
		}
	}
	// Second discard is idempotent (no owned stage → no DELETE, still OK).
	res2 := stager.Discard(context.Background(), buf)
	if !res2.OK() {
		t.Fatalf("idempotent Discard failed: %v", res2)
	}
}

func TestStageLossForbidsReuseNewPasteCreatesNewStage(t *testing.T) {
	secret := plantedToken()
	exp := time.Now().UTC().Add(5 * time.Minute)
	var createN int
	fake := &fakeStageHTTP{
		createFn: func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
			createN++
			id := "stg_ccccccccccccccccccccccccccccccc" + string(rune('0'+createN))
			return buildCreate(201, id, exp, ""), nil
		},
	}
	stager := secretinput.NewStager(fake)
	buf := secretinput.New()
	if err := buf.PasteBytes(secret); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	res1 := stager.Stage(context.Background(), buf)
	if !res1.OK() {
		t.Fatalf("Stage1: %v", res1)
	}
	firstID := res1.StageID

	// Simulate post-consume / expiry observed by host.
	stager.NoteLoss(firstID, secretinput.CodeNotFound)
	if stager.OwnedStageID() != "" {
		t.Fatal("ownership must clear after NoteLoss")
	}

	if err := buf.PasteBytes(secret); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	res2 := stager.Stage(context.Background(), buf)
	if !res2.OK() {
		t.Fatalf("Stage2: %v", res2)
	}
	if res2.StageID == firstID {
		t.Fatalf("must not reuse lost stage ID %q", firstID)
	}
	if stager.OwnedStageID() != res2.StageID {
		t.Fatalf("owned=%q want %q", stager.OwnedStageID(), res2.StageID)
	}
}

func TestResultNeverStringifiesSecret(t *testing.T) {
	secret := plantedToken()
	fake := &fakeStageHTTP{
		createFn: func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
			return nil, errors.New(string(secret))
		},
	}
	stager := secretinput.NewStager(fake)
	buf := secretinput.New()
	if err := buf.PasteBytes(secret); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	res := stager.Stage(context.Background(), buf)
	if res.Code != secretinput.CodeTransport {
		t.Fatalf("Code = %q, want transport", res.Code)
	}
	assertNoSecretLeak(t, secret, res.String(), res.Error(), res.StageID)
}

func TestStageSuccessSubmitsRawOctetsZerosBuffer(t *testing.T) {
	secret := plantedToken()
	exp := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second)
	stageID := "stg_ddddddddddddddddddddddddddddddd4"
	fake := &fakeStageHTTP{
		createFn: func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
			if contentType != "application/octet-stream" {
				t.Fatalf("contentType = %q", contentType)
			}
			if !bytes.Equal(body, secret) {
				t.Fatalf("request body mismatch")
			}
			return buildCreate(201, stageID, exp, ""), nil
		},
	}
	stager := secretinput.NewStager(fake)
	buf := secretinput.New()
	if err := buf.PasteBytes(secret); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}

	res := stager.Stage(context.Background(), buf)
	if !res.OK() {
		t.Fatalf("Stage: %+v", res)
	}
	if res.StageID != stageID {
		t.Fatalf("StageID = %q, want %q", res.StageID, stageID)
	}
	if !res.ExpiresAt.Equal(exp) {
		t.Fatalf("ExpiresAt = %v, want %v", res.ExpiresAt, exp)
	}
	if stager.OwnedStageID() != stageID {
		t.Fatalf("OwnedStageID = %q", stager.OwnedStageID())
	}
	if buf.HasContent() || buf.HasUnstaged() {
		t.Fatal("buffer must be zeroed after successful stage")
	}
	for i := 0; i < buf.Cap(); i++ {
		if buf.ByteAt(i) != 0 {
			t.Fatalf("backing[%d] not zeroed", i)
		}
	}
	if !bytes.Equal(fake.lastCreateBody, secret) {
		t.Fatal("fake did not observe expected raw octets")
	}
	assertNoSecretLeak(t, secret, res.String(), res.Error())
}

func TestSecretScanPackageAndCapturesOmitPlantedToken(t *testing.T) {
	secret := plantedToken()
	exp := time.Now().UTC().Add(5 * time.Minute)
	fake := &fakeStageHTTP{
		createFn: func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
			return buildCreate(201, "stg_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeee5", exp, ""), nil
		},
	}
	stager := secretinput.NewStager(fake)
	buf := secretinput.New()
	if err := buf.PasteBytes(secret); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	res := stager.Stage(context.Background(), buf)
	disc := stager.Discard(context.Background(), buf)

	captures := []string{res.String(), res.Error(), disc.String(), disc.Error(), buf.View(), buf.String()}
	assertNoSecretLeak(t, secret, captures...)

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	needle := string(secret)
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		// Scan production + test sources and any golden-like fixtures.
		switch {
		case strings.HasSuffix(name, ".go"),
			strings.HasSuffix(name, ".golden"),
			strings.HasSuffix(name, ".txt"),
			strings.HasSuffix(name, ".log"):
		default:
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(raw, secret) {
			t.Errorf("planted token appears in %s", name)
		}
		if needle != "" && strings.Contains(string(raw), needle) {
			t.Errorf("planted token string appears in %s", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStageEmptyBufferDoesNotCallHTTP(t *testing.T) {
	fake := &fakeStageHTTP{
		createFn: func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
			t.Fatal("Create must not be called for empty buffer")
			return nil, nil
		},
	}
	stager := secretinput.NewStager(fake)
	res := stager.Stage(context.Background(), secretinput.New())
	if res.Code != secretinput.CodeEmpty {
		t.Fatalf("Code = %q, want empty", res.Code)
	}
	if fake.createCalls != 0 {
		t.Fatalf("createCalls = %d", fake.createCalls)
	}
}

func TestStageMapsBodyTooLargeAndValidation(t *testing.T) {
	secret := plantedToken()
	cases := []struct {
		name string
		resp *generated.CreateProviderSecretStageResponse
		want secretinput.Code
	}{
		{"413", buildCreate(413, "", time.Time{}, "body_too_large"), secretinput.CodeTooLarge},
		{"422", buildCreate(422, "", time.Time{}, "validation_error"), secretinput.CodeUnexpected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeStageHTTP{
				createFn: func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
					return tc.resp, nil
				},
			}
			stager := secretinput.NewStager(fake)
			buf := secretinput.New()
			if err := buf.PasteBytes(secret); err != nil {
				t.Fatalf("PasteBytes: %v", err)
			}
			res := stager.Stage(context.Background(), buf)
			if res.Code != tc.want {
				t.Fatalf("Code = %q, want %q", res.Code, tc.want)
			}
			assertNoSecretLeak(t, secret, res.String(), res.Error())
		})
	}
}

func TestDiscardTransportAndNilResponse(t *testing.T) {
	exp := time.Now().UTC().Add(5 * time.Minute)
	fake := &fakeStageHTTP{
		createFn: func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
			return buildCreate(201, "stg_11111111111111111111111111111111", exp, ""), nil
		},
		deleteFn: func(ctx context.Context, stageID generated.SecretStageID) (*generated.DeleteProviderSecretStageResponse, error) {
			return nil, errors.New("dial failed")
		},
	}
	stager := secretinput.NewStager(fake)
	buf := secretinput.New()
	if err := buf.PasteBytes(plantedToken()); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	if res := stager.Stage(context.Background(), buf); !res.OK() {
		t.Fatalf("Stage: %v", res)
	}
	res := stager.Discard(context.Background(), buf)
	if res.Code != secretinput.CodeTransport {
		t.Fatalf("Code = %q, want transport", res.Code)
	}

	// Re-stage then nil delete response. Prior DELETE must succeed (or be a
	// stage loss) before Stage will Create again; ownership was retained after
	// the transport Discard failure above.
	if err := buf.PasteBytes(plantedToken()); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	fake.deleteFn = func(ctx context.Context, stageID generated.SecretStageID) (*generated.DeleteProviderSecretStageResponse, error) {
		return buildDelete(204, ""), nil
	}
	if res := stager.Stage(context.Background(), buf); !res.OK() {
		t.Fatalf("Stage: %v", res)
	}
	fake.deleteFn = func(ctx context.Context, stageID generated.SecretStageID) (*generated.DeleteProviderSecretStageResponse, error) {
		return nil, nil
	}
	res = stager.Discard(context.Background(), buf)
	if res.Code != secretinput.CodeUnexpected {
		t.Fatalf("Code = %q, want unexpected", res.Code)
	}
}

func TestDiscardDeleteNotFoundIsIdempotentOK(t *testing.T) {
	exp := time.Now().UTC().Add(5 * time.Minute)
	fake := &fakeStageHTTP{
		createFn: func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
			return buildCreate(201, "stg_22222222222222222222222222222222", exp, ""), nil
		},
		deleteFn: func(ctx context.Context, stageID generated.SecretStageID) (*generated.DeleteProviderSecretStageResponse, error) {
			return buildDelete(404, "stage_not_found"), nil
		},
	}
	stager := secretinput.NewStager(fake)
	buf := secretinput.New()
	if err := buf.PasteBytes(plantedToken()); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	if res := stager.Stage(context.Background(), buf); !res.OK() {
		t.Fatalf("Stage: %v", res)
	}
	res := stager.Discard(context.Background(), buf)
	if !res.OK() {
		t.Fatalf("Discard not_found should be idempotent OK, got %v", res)
	}
	if stager.OwnedStageID() != "" {
		t.Fatal("ownership should clear")
	}
}

func TestClearOwnershipAndExpiresAt(t *testing.T) {
	exp := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second)
	fake := &fakeStageHTTP{
		createFn: func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
			return buildCreate(201, "stg_33333333333333333333333333333333", exp, ""), nil
		},
	}
	stager := secretinput.NewStager(fake)
	buf := secretinput.New()
	if err := buf.PasteBytes(plantedToken()); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	if res := stager.Stage(context.Background(), buf); !res.OK() {
		t.Fatalf("Stage: %v", res)
	}
	if !stager.ExpiresAt().Equal(exp) {
		t.Fatalf("ExpiresAt = %v, want %v", stager.ExpiresAt(), exp)
	}
	stager.ClearOwnership()
	if stager.OwnedStageID() != "" || !stager.ExpiresAt().IsZero() {
		t.Fatal("ClearOwnership incomplete")
	}
	stager.NoteLoss("any", secretinput.CodeOK) // no-op
	var nilStager *secretinput.Stager
	if nilStager.OwnedStageID() != "" || !nilStager.ExpiresAt().IsZero() {
		t.Fatal("nil stager accessors")
	}
	nilStager.ClearOwnership()
	nilStager.NoteLoss("any", secretinput.CodeExpired)
	if res := nilStager.Stage(context.Background(), buf); res.Code != secretinput.CodeTransport {
		t.Fatalf("nil Stage code = %q", res.Code)
	}
	if res := nilStager.Discard(context.Background(), buf); !res.OK() {
		t.Fatalf("nil Discard: %v", res)
	}
}

func TestStageNilResponseAndEmptyStageID(t *testing.T) {
	fake := &fakeStageHTTP{
		createFn: func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
			return nil, nil
		},
	}
	stager := secretinput.NewStager(fake)
	buf := secretinput.New()
	if err := buf.PasteBytes(plantedToken()); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	if res := stager.Stage(context.Background(), buf); res.Code != secretinput.CodeUnexpected {
		t.Fatalf("nil resp Code = %q", res.Code)
	}

	fake.createFn = func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
		return buildCreate(201, "", time.Now().UTC(), ""), nil
	}
	if err := buf.PasteBytes(plantedToken()); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	if res := stager.Stage(context.Background(), buf); res.Code != secretinput.CodeUnexpected {
		t.Fatalf("empty id Code = %q", res.Code)
	}
}

func TestIsStageLossHelper(t *testing.T) {
	if !secretinput.IsStageLoss(secretinput.CodeExpired) || !secretinput.IsStageLoss(secretinput.CodeNotFound) {
		t.Fatal("expected loss codes")
	}
	if secretinput.IsStageLoss(secretinput.CodeReserved) || secretinput.IsStageLoss(secretinput.CodeOK) {
		t.Fatal("reserved/ok are not loss")
	}
}

func TestMoveToEndCoverage(t *testing.T) {
	b := secretinput.New()
	if err := b.PasteBytes([]byte{'A', 'B'}); err != nil {
		t.Fatal(err)
	}
	b.MoveToStart()
	b.MoveToEnd()
	if err := b.InsertRune('C'); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, b.Len())
	b.CopyBytes(got)
	if !bytes.Equal(got, []byte{'A', 'B', 'C'}) {
		t.Fatalf("got %v", got)
	}
}

func TestRedactErrorNil(t *testing.T) {
	if secretinput.RedactError(nil) != nil {
		t.Fatal("RedactError(nil) want nil")
	}
}

func TestRestageDiscardsPreviousOwnedStage(t *testing.T) {
	secret := plantedToken()
	exp := time.Now().UTC().Add(5 * time.Minute)
	var n int
	fake := &fakeStageHTTP{
		createFn: func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
			n++
			return buildCreate(201, "stg_ffffffffffffffffffffffffffffff"+string(rune('0'+n)), exp, ""), nil
		},
	}
	stager := secretinput.NewStager(fake)
	buf := secretinput.New()
	if err := buf.PasteBytes(secret); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	res1 := stager.Stage(context.Background(), buf)
	if !res1.OK() {
		t.Fatalf("Stage1: %v", res1)
	}
	if err := buf.PasteBytes(secret); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	res2 := stager.Stage(context.Background(), buf)
	if !res2.OK() {
		t.Fatalf("Stage2: %v", res2)
	}
	if fake.deleteCalls < 1 || fake.deletedIDs[0] != res1.StageID {
		t.Fatalf("expected DELETE of prior stage %q, got calls=%d ids=%v", res1.StageID, fake.deleteCalls, fake.deletedIDs)
	}
	if res2.StageID == res1.StageID {
		t.Fatal("restage must allocate a new stage ID")
	}
}

func TestRestageDeleteTransportRetainsOwnership(t *testing.T) {
	secret := plantedToken()
	exp := time.Now().UTC().Add(5 * time.Minute)
	stageID := "stg_ggggggggggggggggggggggggggggggg7"
	fake := &fakeStageHTTP{
		createFn: func(ctx context.Context, contentType string, body []byte) (*generated.CreateProviderSecretStageResponse, error) {
			return buildCreate(201, stageID, exp, ""), nil
		},
		deleteFn: func(ctx context.Context, id generated.SecretStageID) (*generated.DeleteProviderSecretStageResponse, error) {
			return nil, errors.New("dial timeout")
		},
	}
	stager := secretinput.NewStager(fake)
	buf := secretinput.New()
	if err := buf.PasteBytes(secret); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	res1 := stager.Stage(context.Background(), buf)
	if !res1.OK() {
		t.Fatalf("Stage1: %v", res1)
	}
	createsAfterFirst := fake.createCalls

	if err := buf.PasteBytes(secret); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	res2 := stager.Stage(context.Background(), buf)
	if res2.Code != secretinput.CodeTransport {
		t.Fatalf("Code = %q, want %q", res2.Code, secretinput.CodeTransport)
	}
	if fake.createCalls != createsAfterFirst {
		t.Fatalf("Create must not run after failed DELETE; createCalls=%d want %d", fake.createCalls, createsAfterFirst)
	}
	if fake.deleteCalls < 1 {
		t.Fatal("expected DELETE attempt for prior owned stage")
	}
	if stager.OwnedStageID() != stageID {
		t.Fatalf("owned=%q want retained %q", stager.OwnedStageID(), stageID)
	}
}
