package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type agentStatus struct {
	Name   string   `json:"name"`
	Status string   `json:"status"`
	Skills []string `json:"skills,omitempty"`
}

type agentRequest struct {
	Name string `json:"name"`
}

type taskRequest struct {
	Goal  string `json:"goal"`
	Agent string `json:"agent,omitempty"`
}

type taskResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/agents", s.handleAgents)
	mux.HandleFunc("/agents/start", s.handleAgentStart)
	mux.HandleFunc("/agents/stop", s.handleAgentStop)
	mux.HandleFunc("/tasks", s.handleTasks)
	mux.HandleFunc("/tasks/", s.handleTaskByID)
	mux.HandleFunc("/matter", s.handleMatterDevices)
	mux.HandleFunc("/matter/", s.handleMatterDevice)
	mux.HandleFunc("/matter/commission", s.handleMatterCommission)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var out []agentStatus
	for _, a := range s.registry.List() {
		status := "stopped"
		if runner, ok := s.runners[a.Name]; ok && runner.IsRunning() {
			status = "running"
		}
		out = append(out, agentStatus{
			Name:   a.Name,
			Status: status,
			Skills: a.Skills,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAgentStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req agentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	runner, ok := s.runners[req.Name]
	if !ok {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if err := runner.Start(r.Context()); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": req.Name, "status": "running"})
}

func (s *Server) handleAgentStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req agentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	runner, ok := s.runners[req.Name]
	if !ok {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	runner.Stop()
	writeJSON(w, http.StatusOK, map[string]string{"name": req.Name, "status": "stopped"})
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req taskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.TrimSpace(req.Goal) == "" {
			writeError(w, http.StatusBadRequest, "goal is required")
			return
		}
		task, err := s.dispatcher.Submit(context.Background(), req.Goal, req.Agent)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, taskResponse{ID: task.ID, Status: task.Status})
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.dispatcher.List())
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/tasks/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "task id required")
		return
	}
	task, ok := s.dispatcher.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, taskResponse{
		ID:     task.ID,
		Status: task.Status,
		Result: task.Result,
		Error:  task.Error,
	})
}

func (s *Server) handleMatterDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.matterBot == nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false, "devices": []any{}})
		return
	}
	devices := s.matterBot.GetDevices()
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "devices": devices})
}

func (s *Server) handleMatterDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	entityID := strings.TrimPrefix(r.URL.Path, "/matter/")
	if entityID == "" {
		writeError(w, http.StatusBadRequest, "entity id required")
		return
	}
	if s.matterBot == nil {
		writeError(w, http.StatusServiceUnavailable, "matter not configured")
		return
	}
	entity, ok := s.matterBot.GetState(entityID)
	if !ok {
		writeError(w, http.StatusNotFound, "entity not found")
		return
	}
	writeJSON(w, http.StatusOK, entity)
}

func (s *Server) handleMatterCommission(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.matterBot == nil {
		writeError(w, http.StatusServiceUnavailable, "matter not configured")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "pairing code required")
		return
	}
	if err := s.matterBot.Commission(r.Context(), req.Code); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
