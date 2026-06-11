package api

import (
	"encoding/json"
	"errors"
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
	mux.HandleFunc("/ip-addresses", h.ipAddresses)
	mux.HandleFunc("/spaces", h.spaces)
	mux.HandleFunc("/spaces/", h.spaceDetail)
	return mux
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) prefixes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		prefixes, err := h.service.ListPrefixes(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, prefixes)
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
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *Handler) prefixDetail(w http.ResponseWriter, r *http.Request) {
	id, remainder, ok := parseNestedID(r.URL.Path, "/prefixes/")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid prefix id")
		return
	}

	if r.Method == http.MethodPost && remainder == "allocate-ip" {
		allocated, err := h.service.AllocateNextIP(r.Context(), id)
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, allocated)
		return
	}

	if r.Method == http.MethodPost && remainder == "allocate-subnet" {
		var req struct {
			Size int `json:"size"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		allocated, err := h.service.AllocateSubnet(r.Context(), id, req.Size)
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, allocated)
		return
	}

	if r.Method == http.MethodGet && remainder == "ips" {
		ips, err := h.service.ListIPs(r.Context(), id)
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, ips)
		return
	}

	writeError(w, http.StatusNotFound, "not found")
}

func (h *Handler) ipAddresses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Address  string `json:"address"`
		PrefixID int64  `json:"prefix_id"`
		Status   string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	created, err := h.service.CreateIP(r.Context(), req.Address, req.PrefixID, req.Status)
	if err != nil {
		writeError(w, statusForError(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) spaces(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		spaces, err := h.service.ListSpaces(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, spaces)
	case http.MethodPost:
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		created, err := h.service.CreateSpace(r.Context(), req.Name, req.Description)
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *Handler) spaceDetail(w http.ResponseWriter, r *http.Request) {
	id, remainder, ok := parseNestedID(r.URL.Path, "/spaces/")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid space id")
		return
	}

	if remainder != "blocks" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		blocks, err := h.service.ListBlocks(r.Context(), id)
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, blocks)
	case http.MethodPost:
		var req struct {
			CIDR string `json:"cidr"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		created, err := h.service.CreateBlock(r.Context(), id, req.CIDR)
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func statusForError(err error) int {
	switch {
	case errors.Is(err, ipam.ErrInvalidPrefix), errors.Is(err, ipam.ErrInvalidIPAddress), errors.Is(err, ipam.ErrInvalidSubnetSize), errors.Is(err, ipam.ErrAddressOutOfRange), errors.Is(err, ipam.ErrFamilyMismatch):
		return http.StatusBadRequest
	case errors.Is(err, ipam.ErrPrefixOverlap), errors.Is(err, ipam.ErrBlockOverlap), errors.Is(err, ipam.ErrDuplicateIP), errors.Is(err, ipam.ErrNoAvailableIP), errors.Is(err, ipam.ErrNoAvailableSubnet):
		return http.StatusConflict
	case errors.Is(err, ipam.ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func parseNestedID(path, prefix string) (int64, string, bool) {
	relative := strings.TrimPrefix(path, prefix)
	parts := strings.Split(relative, "/")
	if len(parts) == 0 || parts[0] == "" {
		return 0, "", false
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", false
	}
	if len(parts) == 1 {
		return id, "", true
	}
	return id, strings.Join(parts[1:], "/"), true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
