package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/google/uuid"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/config"
	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/pipeline"
	"github.com/derinbarutcu17/costmaxx/internal/privacy"
	"github.com/derinbarutcu17/costmaxx/internal/reducers"
	"github.com/derinbarutcu17/costmaxx/internal/store"
)

type Server struct {
	cfg        *config.Config
	reducers   *reducers.Registry
	classifier *events.Classifier
	artStore   *artifacts.Store
	db         *store.DB
	redactor   *privacy.Redactor
	sessionID  string
}

func NewServer(cfg *config.Config) (*Server, error) {
	dirs := []string{cfg.Core.DataDir, cfg.Core.LogDir, cfg.Store.ArtifactDir}
	for _, d := range dirs {
		if err := mkdirAll(d, 0700); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	db, err := store.Open(cfg.Store.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	artStore, err := artifacts.NewStore(cfg.Store.ArtifactDir, cfg.Store.MaxArtifactSize)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("artifact store: %w", err)
	}

	return &Server{
		cfg:        cfg,
		reducers:   reducers.NewRegistry(cfg),
		classifier: events.NewClassifier(),
		artStore:   artStore,
		db:         db,
		redactor:   privacy.NewRedactor(),
		sessionID:  "mcp-" + uuid.New().String(),
	}, nil
}

func (s *Server) Close() error {
	return s.db.Close()
}

// Serve runs the newline-delimited JSON-RPC transport (legacy framing).
// Spec-compliant MCP clients use ServeSpec instead.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	// Use a large buffer for potentially large messages
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		s.handleMessage(scanner.Bytes(), w)
	}
	return scanner.Err()
}

// ServeSpec runs the MCP-spec stdio transport: Content-Length framed
// JSON-RPC messages, as required by opencode, Codex, and other spec clients.
func (s *Server) ServeSpec(r io.Reader, w io.Writer) error {
	fr := newFrameReader(r)
	fw := newFrameWriter(w)
	for {
		msg, err := fr.next()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		s.handleMessage(msg, fw)
	}
}

// maxFrameSize bounds inbound MCP frames (matches the legacy scanner cap).
const maxFrameSize = 1024 * 1024

type frameReader struct {
	br *bufio.Reader
}

func newFrameReader(r io.Reader) *frameReader {
	return &frameReader{br: bufio.NewReader(r)}
}

// next reads one Content-Length framed message body.
func (f *frameReader) next() ([]byte, error) {
	contentLength := -1
	for {
		line, err := f.br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("malformed MCP frame header: %q", line)
		}
		if strings.EqualFold(strings.TrimSpace(parts[0]), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid Content-Length: %q", parts[1])
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	if contentLength > maxFrameSize {
		return nil, fmt.Errorf("Content-Length %d exceeds max %d", contentLength, maxFrameSize)
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(f.br, body); err != nil {
		return nil, err
	}
	return body, nil
}

type frameWriter struct {
	w io.Writer
}

func newFrameWriter(w io.Writer) *frameWriter {
	return &frameWriter{w: w}
}

func (f *frameWriter) Write(p []byte) (int, error) {
	if _, err := fmt.Fprintf(f.w, "Content-Length: %d\r\n\r\n", len(p)); err != nil {
		return 0, err
	}
	return f.w.Write(p)
}

// handleMessage parses and dispatches one JSON-RPC message, writing the
// response (if any) through w.
func (s *Server) handleMessage(line []byte, w io.Writer) {
	if len(line) == 0 {
		return
	}

	var req jsonRPCRequest
	if err := json.Unmarshal(line, &req); err != nil {
		writeError(w, json.RawMessage{}, -32700, "Parse error: "+err.Error())
		return
	}

	resp := s.handle(req)
	if req.isNotification() {
		return
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(w, string(data))
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// isNotification returns true if the request has no id field (JSON-RPC notification).
// A request with "id": null is a regular request, not a notification.
func (r *jsonRPCRequest) isNotification() bool {
	return len(r.ID) == 0
}

type toolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

type toolCallResult struct {
	Content []contentItem `json:"content"`
	IsError bool          `json:"isError"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (s *Server) handle(req jsonRPCRequest) jsonRPCResponse {
	id := json.RawMessage(req.ID)
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		return jsonRPCResponse{JSONRPC: "2.0"}
	case "tools/list":
		return s.handleToolList(req)
	case "tools/call":
		return s.handleToolCall(req)
	case "resources/read":
		return s.handleResourceRead(req)
	default:
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &rpcError{Code: -32601, Message: "Method not found: " + req.Method},
		}
	}
}

func (s *Server) handleInitialize(req jsonRPCRequest) jsonRPCResponse {
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": map[string]any{},
			},
			"serverInfo": map[string]string{
				"name":    "costmaxx",
				"version": "1.0.0",
			},
		},
	}
}

func (s *Server) handleToolList(req jsonRPCRequest) jsonRPCResponse {
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"tools": []toolDef{
				{
					Name:        "costmax_run",
					Description: "Execute a local command and return a compact summary. Raw output is stored as a content-addressed artifact and retrievable by the returned artifact_id. Use this instead of Bash when the command may produce large output that the model does not need to see in full.",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"command": map[string]any{
								"type":        "string",
								"description": "Shell command to execute",
							},
							"cwd": map[string]any{
								"type":        "string",
								"description": "Working directory (defaults to current directory)",
							},
						},
						"required": []string{"command"},
					},
				},
			},
		},
	}
}

func (s *Server) handleToolCall(req jsonRPCRequest) jsonRPCResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, -32602, "Invalid params: "+err.Error())
	}

	if params.Name != "costmax_run" {
		return errorResponse(req.ID, -32602, "Unknown tool: "+params.Name)
	}

	var args struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd,omitempty"`
	}
	if err := json.Unmarshal(params.Arguments, &args); err != nil {
		return errorResponse(req.ID, -32602, "Invalid arguments: "+err.Error())
	}

	if args.Command == "" {
		return errorResponse(req.ID, -32602, "command is required")
	}

	result, err := s.execute(args.Command, args.Cwd)
	if err != nil {
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: &toolCallResult{
				Content: []contentItem{{Type: "text", Text: err.Error()}},
				IsError: true,
			},
		}
	}

	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

type runResult struct {
	CompactText   string `json:"compact_text"`
	ArtifactID    string `json:"artifact_id"`
	RawTokens     int    `json:"raw_estimated_tokens"`
	CompactTokens int    `json:"compact_estimated_tokens"`
}

func (s *Server) execute(command, cwd string) (*toolCallResult, error) {
	cmd := exec.Command("sh", "-c", command)
	if cwd != "" {
		cmd.Dir = cwd
	}

	// CombinedOutput preserves stderr for successful commands as well as
	// failing commands. Dropping successful stderr would make the stored
	// evidence and model-visible summary incomplete.
	raw, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			// ExitCode() returns -1 when the process died by signal. Report
			// the shell convention (128+signal) so evidence keeps the cause.
			if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				exitCode = 128 + int(ws.Signal())
			}
		} else {
			return nil, fmt.Errorf("exec error: %w", err)
		}
	}

	output := string(raw)

	// Delegate the shared ingestion chain (redact, store, classify, reduce,
	// recommend, guard, metrics) to the shared pipeline so the MCP tool and
	// the CLI artifact add command emit byte-identical envelopes.
	responseText, err := pipeline.Process(pipeline.Deps{
		Store:      s.artStore,
		DB:         s.db,
		Classifier: s.classifier,
		Registry:   s.reducers,
		Redactor:   s.redactor,
		SessionID:  s.sessionID,
	}, output, command, cwd, exitCode, "mcp_costmax_run")
	if err != nil {
		return nil, err
	}

	return &toolCallResult{
		Content: []contentItem{{
			Type: "text",
			Text: responseText,
		}},
		// A nonzero command exit is evidence, not an MCP transport failure.
		// The model must receive diagnostics for expected failing tests/builds.
		IsError: false,
	}, nil
}

func (s *Server) handleResourceRead(req jsonRPCRequest) jsonRPCResponse {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.URI == "" {
		return errorResponse(req.ID, -32602, "Invalid params: uri required")
	}

	// Expected URI format: cmx://artifact/<artifact-id>
	artifactID := strings.TrimPrefix(params.URI, "cmx://artifact/")
	if artifactID == params.URI {
		return errorResponse(req.ID, -32602, "Unknown resource: "+params.URI)
	}

	meta, err := s.db.GetArtifact(artifactID)
	if err != nil {
		return errorResponse(req.ID, -32602, "Artifact not found: "+err.Error())
	}
	if meta == nil {
		return errorResponse(req.ID, -32602, "Artifact not found: "+artifactID)
	}

	raw, err := s.artStore.RetrieveByDigest(meta.ContentDigest)
	if err != nil {
		return errorResponse(req.ID, -32602, "Artifact read error: "+err.Error())
	}

	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"contents": []map[string]any{
				{
					"uri":      params.URI,
					"mimeType": "text/plain",
					"text":     string(raw),
				},
			},
		},
	}
}

func errorResponse(id json.RawMessage, code int, msg string) jsonRPCResponse {
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      rawToID(id),
		Error:   &rpcError{Code: code, Message: msg},
	}
}

func writeError(w io.Writer, id json.RawMessage, code int, msg string) {
	resp := errorResponse(id, code, msg)
	data, _ := json.Marshal(resp)
	fmt.Fprintln(w, string(data))
}

func rawToID(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		if i, err := n.Int64(); err == nil {
			return i
		}
		return n.String()
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return nil
}

func mkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// GetArtifact exposes the artifact store lookup for tests.
func (s *Server) GetArtifact(id string) (*artifacts.EvidenceArtifact, error) {
	return s.db.GetArtifact(id)
}

// GetArtifactStore exposes the artifact store for tests.
func (s *Server) GetArtifactStore() *artifacts.Store {
	return s.artStore
}

// ReductionCount exposes persisted reduction cardinality for integration
// checks without exposing the database handle.
func (s *Server) ReductionCount() (int, error) {
	return s.db.ReductionCount()
}

// SessionID identifies the long-lived MCP server process for metrics queries.
func (s *Server) SessionID() string {
	return s.sessionID
}

// SessionMetrics exposes the process-scoped accumulation used by report
// tooling and integration checks.
func (s *Server) SessionMetrics() (rawTokens, compactTokens, artifactsReduced, toolCalls int, err error) {
	return s.db.GetSessionMetrics(s.sessionID)
}
