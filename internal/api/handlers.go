package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"simple-ipam/internal/ipam"
)

// Handler wires the minimal HTTP surface for the MVP.
type Handler struct {
	service *ipam.Service
}

func NewHandler(service *ipam.Service) http.Handler {
	h := &Handler{service: service}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/prefixes", h.prefixes)
	mux.HandleFunc("/prefixes/", h.prefixDetail)
	return mux
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) prefixes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		prefixes, err := h.service.ListPrefixes(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		json.NewEncoder(w).Encode(prefixes)
	case http.MethodPost:
		var req struct {
			CIDR string `json:"cidr"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		created, err := h.service.CreatePrefix(r.Context(), req.CIDR)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(created)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *Handler) prefixDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/prefixes/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "invalid prefix id")
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid prefix id")
		return
	}

	if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "allocate-ip" {
		allocated, err := h.service.AllocateNextIP(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(allocated)
		return
	}

	if r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "ips" {
		ips, err := h.service.ListIPs(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		json.NewEncoder(w).Encode(ips)
		return
	}

	writeError(w, http.StatusNotFound, "not found")
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
