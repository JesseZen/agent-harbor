package main

import (
	"archive/tar"
	"compress/gzip"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"filippo.io/age"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

const (
	maxPortableFile  = 8 << 20
	maxPortableTotal = 64 << 20
	archiveVersion   = 1
)

type portableFile struct {
	Path string `json:"path"`
	Mode uint32 `json:"mode"`
	Data []byte `json:"data"`
}
type portableArchive struct {
	Version      int                   `json:"version"`
	CreatedAt    time.Time             `json:"created_at"`
	SourceOS     string                `json:"source_os"`
	SourceArch   string                `json:"source_arch"`
	Files        []portableFile        `json:"files"`
	Requirements []portableRequirement `json:"requirements,omitempty"`
}
type portableRequirement struct {
	Kind      string `json:"kind"`
	Detail    string `json:"detail"`
	Available bool   `json:"available"`
}

func commandExport(manifest bundleManifest, paths productPaths, args []string, stdout io.Writer) error {
	options, output, err := parsePortableFlags(args, false)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(output); err == nil {
		return fmt.Errorf("refusing to overwrite existing export %s", output)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	archive, err := collectPortableArchive(paths, options.materialize)
	if err != nil {
		return err
	}
	defer archive.zero()
	recipients, err := options.recipients()
	if err != nil {
		return err
	}
	destination, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		destination.Close()
		if !committed {
			_ = os.Remove(output)
		}
	}()
	writer, err := age.Encrypt(destination, recipients...)
	if err != nil {
		return err
	}
	if err := writeArchive(writer, archive); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := destination.Sync(); err != nil {
		return err
	}
	committed = true
	fmt.Fprintf(stdout, "Encrypted portable export written to %s (%d files).\n", output, len(archive.Files))
	return nil
}

func commandImport(manifest bundleManifest, paths productPaths, args []string, stdout io.Writer) error {
	options, input, err := parsePortableFlags(args, true)
	if err != nil {
		return err
	}
	lock, err := acquirePortableImportLock(paths)
	if err != nil {
		return err
	}
	defer lock.Close()
	empty, err := portableTargetEmpty(paths)
	if err != nil {
		return err
	}
	if !empty {
		return errors.New("import target is not empty; refusing to merge or overwrite existing Agent Harbor data")
	}
	identities, err := options.identities()
	if err != nil {
		return err
	}
	reader, closeInput, err := openPortableInput(input)
	if err != nil {
		return err
	}
	defer closeInput()
	decrypted, err := age.Decrypt(reader, identities...)
	if err != nil {
		return fmt.Errorf("decrypt portable archive: %w", err)
	}
	archive, err := readArchive(decrypted)
	if err != nil {
		return err
	}
	defer archive.zero()
	if archive.Version != archiveVersion {
		return fmt.Errorf("unsupported portable archive version %d", archive.Version)
	}
	if _, ok := manifest.componentByRole("runtime.core"); ok {
		if err := ensureRuntime(manifest, paths); err != nil {
			return err
		}
	}
	instanceID, err := newPortableInstanceID()
	if err != nil {
		return err
	}
	if err := commitPortableArchive(manifest, paths, archive, instanceID); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Imported encrypted portable archive into %s.\n", paths.ConfigRoot)
	return nil
}

func acquirePortableImportLock(paths productPaths) (*os.File, error) {
	parent := filepath.Dir(paths.ConfigRoot)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, err
	}
	lock, err := openPrivateFile(filepath.Join(parent, ".agent-harbor-import.lock"))
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("lock portable import: %w", err)
	}
	return lock, nil
}

type portableOptions struct {
	recipientsValues             []string
	passphraseFile, identityFile string
	materialize                  bool
	importMode                   bool
}

func parsePortableFlags(args []string, importMode bool) (portableOptions, string, error) {
	options := portableOptions{importMode: importMode}
	positional := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--recipient":
			if i+1 >= len(args) {
				return options, "", usageError("--recipient needs an age recipient")
			}
			options.recipientsValues = append(options.recipientsValues, args[i+1])
			i++
		case "--passphrase-file":
			if i+1 >= len(args) {
				return options, "", usageError("--passphrase-file needs a path")
			}
			options.passphraseFile = args[i+1]
			i++
		case "--identity-file":
			if i+1 >= len(args) {
				return options, "", usageError("--identity-file needs a path")
			}
			options.identityFile = args[i+1]
			i++
		case "--materialize-external-secrets":
			options.materialize = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return options, "", usageError("unknown portable archive option " + args[i])
			}
			if positional != "" {
				return options, "", usageError("portable command accepts one FILE")
			}
			positional = args[i]
		}
	}
	if positional == "" {
		return options, "", usageError("portable command requires FILE or -")
	}
	if !importMode && options.identityFile != "" {
		return options, "", usageError("--identity-file is only valid for import")
	}
	if importMode && len(options.recipientsValues) != 0 {
		return options, "", usageError("--recipient is only valid for export")
	}
	return options, positional, nil
}
func (o portableOptions) recipients() ([]age.Recipient, error) {
	if len(o.recipientsValues) > 0 && o.passphraseFile != "" {
		return nil, usageError("choose --recipient or --passphrase-file, not both")
	}
	if len(o.recipientsValues) > 0 {
		result := make([]age.Recipient, 0, len(o.recipientsValues))
		for _, value := range o.recipientsValues {
			recipient, err := age.ParseX25519Recipient(value)
			if err != nil {
				return nil, fmt.Errorf("invalid age recipient: %w", err)
			}
			result = append(result, recipient)
		}
		return result, nil
	}
	password, err := readPassphrase(o.passphraseFile)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(password)
	recipient, err := age.NewScryptRecipient(string(password))
	if err != nil {
		return nil, err
	}
	return []age.Recipient{recipient}, nil
}
func (o portableOptions) identities() ([]age.Identity, error) {
	if o.identityFile != "" && o.passphraseFile != "" {
		return nil, usageError("choose --identity-file or --passphrase-file, not both")
	}
	if o.identityFile != "" {
		source, err := os.Open(o.identityFile)
		if err != nil {
			return nil, err
		}
		defer source.Close()
		return age.ParseIdentities(source)
	}
	password, err := readPassphrase(o.passphraseFile)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(password)
	identity, err := age.NewScryptIdentity(string(password))
	if err != nil {
		return nil, err
	}
	return []age.Identity{identity}, nil
}
func readPassphrase(path string) ([]byte, error) {
	if path != "" {
		value, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return []byte(strings.TrimRight(string(value), "\r\n")), nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, errors.New("portable encryption needs --passphrase-file when stdin is not a terminal")
	}
	fmt.Fprint(os.Stderr, "Agent Harbor archive passphrase: ")
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return value, err
}
func zeroBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

func collectPortableArchive(paths productPaths, materialize bool) (portableArchive, error) {
	archive := portableArchive{Version: archiveVersion, CreatedAt: time.Now().UTC(), SourceOS: runtime.GOOS, SourceArch: runtime.GOARCH}
	coreCollected := false
	if readiness, err := readReadiness(paths); err == nil && readiness.Snapshot.Generation > 0 {
		request, _ := json.Marshal(map[string]any{"instance_id": readiness.Identity.InstanceID, "expected_generation": readiness.Snapshot.Generation, "materialize_external_secrets": materialize})
		var snapshot struct {
			SourceYAML []byte `json:"source_yaml"`
			Secrets    []struct {
				CredentialID   string `json:"credential_id"`
				ManagedAccount string `json:"managed_account"`
				Value          []byte `json:"value"`
			} `json:"secrets"`
			Requirements []portableRequirement `json:"requirements"`
		}
		if _, requestErr := adminRequest(paths, "POST", "/v1/portable/export", request, &snapshot); requestErr == nil {
			coreCollected = true
			source := snapshot.SourceYAML
			if materialize {
				var materializeErr error
				source, materializeErr = materializeSnapshotConfig(source, snapshot.Secrets)
				if materializeErr != nil {
					return archive, materializeErr
				}
			}
			archive.Files = append(archive.Files, portableFile{Path: "core/config.yaml", Mode: 0600, Data: source})
			archive.Requirements = snapshot.Requirements
			for _, secret := range snapshot.Secrets {
				if secret.ManagedAccount == "" {
					continue
				}
				parts := strings.Split(secret.ManagedAccount, "/")
				if len(parts) == 4 {
					archive.Files = append(archive.Files, portableFile{Path: filepath.ToSlash(filepath.Join("core/secrets", parts[1], parts[2], parts[3]+".secret")), Mode: 0600, Data: secret.Value})
				}
			}
		} else if !strings.Contains(requestErr.Error(), "HTTP 404") {
			return archive, fmt.Errorf("Core portable snapshot: %w", requestErr)
		}
	}
	if !coreCollected {
		config := filepath.Join(paths.ConfigRoot, "config.yaml")
		if source, err := os.ReadFile(config); err == nil {
			if materialize {
				source, err = materializeExternalSecrets(source, &archive.Files)
				if err != nil {
					return archive, err
				}
				source, err = promoteMaterializedFiles(source, &archive.Files)
				if err != nil {
					return archive, err
				}
			}
			archive.Files = append(archive.Files, portableFile{Path: "core/config.yaml", Mode: 0600, Data: source})
			if !materialize {
				archive.Requirements, _ = scanExternalRequirementsBytes(source)
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return archive, err
		}
		if err := collectAllowlisted(filepath.Join(paths.StateRoot, "secrets"), "core/secrets", &archive.Files); err != nil {
			return archive, err
		}
	}
	for _, spec := range []struct{ root, prefix string }{{paths.TUIConfig, "tui/config"}, {paths.TUIData, "tui/data"}} {
		if err := collectAllowlisted(spec.root, spec.prefix, &archive.Files); err != nil {
			return archive, err
		}
	}
	if materialize {
		archive.Requirements = nil
	}
	total := 0
	for _, file := range archive.Files {
		total += len(file.Data)
		if total > maxPortableTotal {
			return archive, errors.New("portable export exceeds total size limit")
		}
	}
	return archive, nil
}

func promoteMaterializedFiles(source []byte, files *[]portableFile) ([]byte, error) {
	instanceID, err := newPortableInstanceID()
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err := yaml.Unmarshal(source, &document); err != nil {
		return nil, err
	}
	credentials, ok := document["credentials"].([]any)
	if !ok {
		return source, nil
	}
	for _, item := range credentials {
		credential, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := credential["id"].(string)
		generation, _ := credential["generation"].(int)
		secret, _ := credential["secret"].(map[string]any)
		file, _ := secret["file"].(map[string]any)
		path, _ := file["path"].(string)
		prefix := "__AGENT_HARBOR_STATE_ROOT__/secrets/external/"
		if id == "" || generation < 1 || !strings.HasPrefix(path, prefix) {
			continue
		}
		oldArchivePath := "core/secrets/external/" + strings.TrimPrefix(path, prefix)
		newArchivePath := filepath.ToSlash(filepath.Join("core/secrets", instanceID, id, fmt.Sprintf("%d.secret", generation)))
		for index := range *files {
			if (*files)[index].Path == oldArchivePath {
				(*files)[index].Path = newArchivePath
			}
		}
		secret["exportable"] = false
		delete(secret, "file")
		secret["keychain"] = map[string]any{"service": "agent-harbor", "account": fmt.Sprintf("agent-harbor/%s/%s/%d", instanceID, id, generation)}
	}
	return yaml.Marshal(document)
}

func materializeSnapshotConfig(source []byte, secrets []struct {
	CredentialID   string `json:"credential_id"`
	ManagedAccount string `json:"managed_account"`
	Value          []byte `json:"value"`
}) ([]byte, error) {
	var document map[string]any
	if err := yaml.Unmarshal(source, &document); err != nil {
		return nil, err
	}
	accounts := make(map[string]string, len(secrets))
	for _, secret := range secrets {
		if secret.CredentialID != "" && secret.ManagedAccount != "" {
			accounts[secret.CredentialID] = secret.ManagedAccount
		}
	}
	credentials, ok := document["credentials"].([]any)
	if !ok {
		return nil, errors.New("portable snapshot has no credentials array")
	}
	for _, item := range credentials {
		credential, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := credential["id"].(string)
		account := accounts[id]
		if account == "" {
			continue
		}
		credential["secret"] = map[string]any{"exportable": false, "keychain": map[string]any{"service": "agent-harbor", "account": account}}
	}
	return yaml.Marshal(document)
}

func (archive *portableArchive) zero() {
	for index := range archive.Files {
		zeroBytes(archive.Files[index].Data)
	}
}
func collectAllowlisted(root, prefix string, files *[]portableFile) error {
	info, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("portable source is not a private directory: %s", root)
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if !portableTUIPathAllowed(prefix, relative, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("portable export rejects symlink %s", path)
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("portable export rejects non-regular file %s", path)
		}
		if info.Size() > maxPortableFile {
			return fmt.Errorf("portable file too large: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		*files = append(*files, portableFile{Path: filepath.ToSlash(filepath.Join(prefix, relative)), Mode: uint32(info.Mode().Perm()), Data: data})
		return nil
	})
}

func portableTUIPathAllowed(prefix, relative string, directory bool) bool {
	relative = filepath.ToSlash(relative)
	if prefix == "core/secrets" {
		return true
	}
	if prefix == "tui/config" {
		return directory || relative == "config.toml" || relative == "config.json"
	}
	if prefix != "tui/data" {
		return false
	}
	for _, root := range []string{"skills", "conductor", "watcher", "watchers"} {
		if relative == root || strings.HasPrefix(relative, root+"/") {
			return true
		}
	}
	return false
}
func portableTargetEmpty(paths productPaths) (bool, error) {
	for _, root := range []string{paths.ConfigRoot, paths.StateRoot, paths.TUIConfig, paths.TUIData} {
		entries, err := os.ReadDir(root)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if len(entries) > 0 {
			return false, nil
		}
	}
	return true, nil
}

func writeArchive(destination io.Writer, archive portableArchive) error {
	gzipWriter := gzip.NewWriter(destination)
	tarWriter := tar.NewWriter(gzipWriter)
	headerData, err := json.Marshal(archive)
	if err != nil {
		return err
	}
	if err := tarWriter.WriteHeader(&tar.Header{Name: "portable.json", Mode: 0600, Size: int64(len(headerData))}); err != nil {
		return err
	}
	if _, err := tarWriter.Write(headerData); err != nil {
		return err
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	return gzipWriter.Close()
}
func readArchive(source io.Reader) (portableArchive, error) {
	gzipReader, err := gzip.NewReader(source)
	if err != nil {
		return portableArchive{}, err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	header, err := tarReader.Next()
	if err != nil {
		return portableArchive{}, err
	}
	if header.Name != "portable.json" || header.Typeflag != tar.TypeReg || header.Size > maxPortableTotal {
		return portableArchive{}, errors.New("invalid portable archive manifest")
	}
	data, err := io.ReadAll(io.LimitReader(tarReader, maxPortableTotal+1))
	if err != nil || int64(len(data)) != header.Size {
		return portableArchive{}, errors.New("portable archive manifest exceeds limit")
	}
	var archive portableArchive
	if err := json.Unmarshal(data, &archive); err != nil {
		return portableArchive{}, err
	}
	total := 0
	for _, file := range archive.Files {
		if file.Path == "" || filepath.IsAbs(file.Path) || !filepath.IsLocal(filepath.FromSlash(file.Path)) || strings.Contains(file.Path, "..") || len(file.Data) > maxPortableFile {
			return portableArchive{}, errors.New("invalid portable archive file")
		}
		total += len(file.Data)
		if total > maxPortableTotal {
			return portableArchive{}, errors.New("portable archive exceeds total size limit")
		}
	}
	return archive, nil
}
func newPortableInstanceID() (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(cryptorand.Reader, value); err != nil {
		return "", err
	}
	return "ins_" + hex.EncodeToString(value), nil
}

func commitPortableArchive(manifest bundleManifest, paths productPaths, archive portableArchive, instanceID string) error {
	originals := map[string]bool{}
	for _, root := range []string{paths.ConfigRoot, paths.StateRoot, paths.TUIConfig, paths.TUIData} {
		_, err := os.Lstat(root)
		originals[root] = err == nil
	}
	createdFiles := []string{}
	committed := false
	defer func() {
		if committed {
			return
		}
		for i := len(createdFiles) - 1; i >= 0; i-- {
			_ = os.Remove(createdFiles[i])
		}
		for _, root := range []string{paths.ConfigRoot, paths.StateRoot, paths.TUIConfig, paths.TUIData} {
			if !originals[root] {
				_ = os.RemoveAll(root)
			} else {
				_ = removeEmptyDirectories(root)
			}
		}
	}()
	if err := ensurePrivatePath(paths.ConfigRoot); err != nil {
		return err
	}
	if err := ensurePrivatePath(paths.StateRoot); err != nil {
		return err
	}
	var configData []byte
	for _, file := range archive.Files {
		var destination string
		switch {
		case file.Path == "core/config.yaml":
			configData = append([]byte(nil), file.Data...)
			continue
		case strings.HasPrefix(file.Path, "core/secrets/"):
			relative := strings.TrimPrefix(file.Path, "core/secrets/")
			parts := strings.Split(relative, "/")
			if len(parts) == 4 && parts[0] != "external" {
				parts[0] = instanceID
				relative = strings.Join(parts, "/")
			}
			destination = filepath.Join(paths.StateRoot, "secrets", relative)
		case strings.HasPrefix(file.Path, "tui/config/"):
			destination = filepath.Join(paths.TUIConfig, strings.TrimPrefix(file.Path, "tui/config/"))
		case strings.HasPrefix(file.Path, "tui/data/"):
			destination = filepath.Join(paths.TUIData, strings.TrimPrefix(file.Path, "tui/data/"))
		default:
			continue
		}
		if err := atomicPrivateWrite(destination, file.Data); err != nil {
			return err
		}
		createdFiles = append(createdFiles, destination)
	}
	if len(configData) == 0 {
		return errors.New("portable archive has no Core configuration")
	}
	rewritten, err := rewritePortableConfig(configData, paths.StateRoot, instanceID)
	if err != nil {
		return err
	}
	if err := atomicPrivateWrite(filepath.Join(paths.ConfigRoot, "config.yaml"), rewritten); err != nil {
		return err
	}
	createdFiles = append(createdFiles, filepath.Join(paths.ConfigRoot, "config.yaml"))
	if _, ok := manifest.componentByRole("runtime.core"); ok {
		if err := initializePortableCore(manifest, paths, instanceID); err != nil {
			return err
		}
	}
	committed = true
	return nil
}

func initializePortableCore(manifest bundleManifest, paths productPaths, instanceID string) error {
	corePath, err := runtimeExecutable(manifest, paths, "runtime.core")
	if err != nil {
		return err
	}
	command := exec.Command(corePath, "portable-init", "--config-dir", paths.ConfigRoot, "--state-root", paths.StateRoot, "--instance-id", instanceID)
	command.Env = runtimeEnvironment(manifest, paths, terminfoRoot(manifest, paths))
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Core portable initialization: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func removeEmptyDirectories(root string) error {
	directories := []string{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != root {
			directories = append(directories, path)
		}
		return nil
	}); err != nil {
		return err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		_ = os.Remove(directories[i])
	}
	return nil
}

func rewritePortableConfig(source []byte, stateRoot, instanceID string) ([]byte, error) {
	var document map[string]any
	if err := yaml.Unmarshal(source, &document); err != nil {
		return nil, err
	}
	if instance, ok := document["instance"].(map[string]any); ok {
		instance["state_root"] = stateRoot
	}
	var rewrite func(any)
	rewrite = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			for key, child := range value {
				if stringValue, ok := child.(string); ok && strings.HasPrefix(stringValue, "__AGENT_HARBOR_STATE_ROOT__/") {
					value[key] = filepath.Join(stateRoot, strings.TrimPrefix(stringValue, "__AGENT_HARBOR_STATE_ROOT__/"))
				}
			}
			if file, ok := value["file"].(map[string]any); ok {
				if sourcePath, ok := file["path"].(string); ok {
					if rewrittenPath, managed := rewriteManagedFilePath(sourcePath, stateRoot, instanceID); managed {
						file["path"] = rewrittenPath
					}
				}
			}
			if keychain, ok := value["keychain"].(map[string]any); ok {
				service, _ := keychain["service"].(string)
				account, _ := keychain["account"].(string)
				if service == "agent-harbor" {
					rewrittenAccount := rewriteManagedAccount(account, instanceID)
					if path, ok := managedAccountPath(stateRoot, rewrittenAccount); ok {
						delete(value, "keychain")
						value["file"] = map[string]any{"path": path}
					}
				}
			}
			for _, child := range value {
				rewrite(child)
			}
		case []any:
			for _, child := range value {
				rewrite(child)
			}
		}
	}
	rewrite(document)
	return yaml.Marshal(document)
}

func rewriteManagedAccount(account, instanceID string) string {
	parts := strings.Split(account, "/")
	if len(parts) == 4 && parts[0] == "agent-harbor" && instanceID != "" {
		parts[1] = instanceID
		return strings.Join(parts, "/")
	}
	return account
}

func rewriteManagedFilePath(sourcePath, stateRoot, instanceID string) (string, bool) {
	if !filepath.IsAbs(sourcePath) || instanceID == "" {
		return "", false
	}
	clean := filepath.Clean(sourcePath)
	parts := strings.Split(filepath.ToSlash(clean), "/")
	if len(parts) < 4 {
		return "", false
	}
	secretIndex := -1
	for index := range parts {
		if parts[index] == "secrets" {
			secretIndex = index
		}
	}
	if secretIndex < 0 || len(parts)-secretIndex != 4 || !strings.HasSuffix(parts[secretIndex+3], ".secret") {
		return "", false
	}
	oldInstance, credential, generation := parts[secretIndex+1], parts[secretIndex+2], strings.TrimSuffix(parts[secretIndex+3], ".secret")
	if _, err := parsePortableInstanceID(oldInstance); err != nil || credential == "" || generation == "" {
		return "", false
	}
	if _, err := parsePortableInstanceID(instanceID); err != nil {
		return "", false
	}
	return filepath.Join(stateRoot, "secrets", instanceID, credential, generation+".secret"), true
}

func parsePortableInstanceID(value string) (string, error) {
	if len(value) != 36 || !strings.HasPrefix(value, "ins_") {
		return "", errors.New("invalid instance ID")
	}
	for _, char := range value[4:] {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return "", errors.New("invalid instance ID")
		}
	}
	return value, nil
}

func materializeExternalSecrets(source []byte, files *[]portableFile) ([]byte, error) {
	var document map[string]any
	if err := yaml.Unmarshal(source, &document); err != nil {
		return nil, err
	}
	index := 0
	var walk func(any) error
	walk = func(node any) error {
		switch value := node.(type) {
		case map[string]any:
			if env, ok := value["env"].(map[string]any); ok {
				if name, _ := env["name"].(string); name != "" {
					secret, exists := os.LookupEnv(name)
					if !exists || secret == "" {
						return fmt.Errorf("external environment secret %s is unavailable", name)
					}
					index++
					archivePath := fmt.Sprintf("core/secrets/external/%d.secret", index)
					*files = append(*files, portableFile{Path: archivePath, Mode: 0600, Data: []byte(secret)})
					delete(value, "env")
					value["file"] = map[string]any{"path": "__AGENT_HARBOR_STATE_ROOT__/secrets/external/" + fmt.Sprintf("%d.secret", index)}
				}
			}
			if file, ok := value["file"].(map[string]any); ok {
				if path, _ := file["path"].(string); path != "" && !strings.HasPrefix(path, "__AGENT_HARBOR_STATE_ROOT__/") {
					secret, err := os.ReadFile(path)
					if err != nil {
						return fmt.Errorf("materialize external file secret %s: %w", path, err)
					}
					if len(secret) == 0 {
						return fmt.Errorf("materialize external file secret %s: file is empty", path)
					}
					{
						index++
						archivePath := fmt.Sprintf("core/secrets/external/%d.secret", index)
						*files = append(*files, portableFile{Path: archivePath, Mode: 0600, Data: secret})
						file["path"] = "__AGENT_HARBOR_STATE_ROOT__/secrets/external/" + fmt.Sprintf("%d.secret", index)
					}
				}
			}
			if keychain, ok := value["keychain"].(map[string]any); ok {
				service, _ := keychain["service"].(string)
				account, _ := keychain["account"].(string)
				if service != "agent-harbor" {
					secret, err := readExternalKeychain(service, account)
					if err != nil {
						return err
					}
					index++
					archivePath := fmt.Sprintf("core/secrets/external/%d.secret", index)
					*files = append(*files, portableFile{Path: archivePath, Mode: 0600, Data: secret})
					delete(value, "keychain")
					value["file"] = map[string]any{"path": "__AGENT_HARBOR_STATE_ROOT__/secrets/external/" + fmt.Sprintf("%d.secret", index)}
				}
			}
			for _, child := range value {
				if err := walk(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range value {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(document); err != nil {
		return nil, err
	}
	return yaml.Marshal(document)
}

func readExternalKeychain(service, account string) ([]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("external keychain materialization is only supported on macOS in v1")
	}
	if service == "" || account == "" {
		return nil, errors.New("external keychain locator is incomplete")
	}
	value, err := exec.Command("/usr/bin/security", "find-generic-password", "-s", service, "-a", account, "-w").Output()
	if err != nil {
		return nil, fmt.Errorf("materialize external Keychain secret %s/%s: %w", service, account, err)
	}
	value = []byte(strings.TrimRight(string(value), "\r\n"))
	if len(value) == 0 {
		return nil, errors.New("external keychain secret is empty")
	}
	return value, nil
}

func managedAccountPath(stateRoot, account string) (string, bool) {
	parts := strings.Split(account, "/")
	if len(parts) != 4 || parts[0] != "agent-harbor" || parts[1] == "" || parts[2] == "" || parts[3] == "" {
		return "", false
	}
	for _, part := range parts[1:] {
		if part == "." || part == ".." || strings.ContainsAny(part, "/\\\x00") {
			return "", false
		}
	}
	return filepath.Join(stateRoot, "secrets", parts[1], parts[2], parts[3]+".secret"), true
}
func openPortableInput(path string) (io.Reader, func(), error) {
	if path == "-" {
		return os.Stdin, func() {}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return file, func() { _ = file.Close() }, nil
}
func scanExternalRequirements(config string) ([]portableRequirement, error) {
	source, err := os.ReadFile(config)
	if err != nil {
		return nil, err
	}
	return scanExternalRequirementsBytes(source)
}
func scanExternalRequirementsBytes(source []byte) ([]portableRequirement, error) {
	var value map[string]any
	if err := yaml.Unmarshal(source, &value); err != nil {
		return nil, err
	}
	result := []portableRequirement{}
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			if env, ok := typed["env"].(map[string]any); ok {
				if name, _ := env["name"].(string); name != "" {
					_, available := os.LookupEnv(name)
					result = append(result, portableRequirement{Kind: "env", Detail: name, Available: available})
				}
			}
			if file, ok := typed["file"].(map[string]any); ok {
				if path, _ := file["path"].(string); path != "" {
					_, err := os.Stat(path)
					result = append(result, portableRequirement{Kind: "file", Detail: path, Available: err == nil})
				}
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return result, nil
}
