package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/memql/graph-service/internal/graph"
	"github.com/memql/graph-service/internal/kafka"
	"github.com/memql/graph-service/internal/neo4j"
)

// Handler handles HTTP requests for the graph service
type Handler struct {
	neo4j    *neo4j.Client
	producer *kafka.Producer
	logger   *zap.Logger
}

// NewHandler creates a new API handler
func NewHandler(
	neo4jClient *neo4j.Client,
	producer *kafka.Producer,
	logger *zap.Logger,
) *Handler {
	return &Handler{
		neo4j:    neo4jClient,
		producer: producer,
		logger:   logger,
	}
}

// RegisterRoutes registers all API routes
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Route("/knowledge-graph", func(r chi.Router) {
		r.Post("/query", h.QueryGraph)
		r.Post("/relationships", h.CreateRelationship)
		r.Post("/paths", h.FindPaths)
		r.Post("/neighbors", h.GetNeighbors)
		r.Get("/statistics", h.GetStatistics)
		r.Post("/import", h.ImportData)
		r.Post("/export", h.ExportData)
	})
}

// QueryGraph executes a Cypher query
func (h *Handler) QueryGraph(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		h.error(w, "missing tenant ID", http.StatusBadRequest)
		return
	}

	// Parse request
	var req graph.GraphQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Query == "" {
		h.error(w, "query is required", http.StatusBadRequest)
		return
	}

	// TODO: Validate and sanitize Cypher query to prevent injection

	// Execute query
	result, err := h.neo4j.ExecuteQuery(ctx, tenantID, req.Query, req.Parameters)
	if err != nil {
		h.logger.Error("failed to execute query", zap.Error(err))
		h.error(w, "failed to execute query", http.StatusInternalServerError)
		return
	}

	h.json(w, result, http.StatusOK)
}

// CreateRelationship creates a relationship between nodes
func (h *Handler) CreateRelationship(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		h.error(w, "missing tenant ID", http.StatusBadRequest)
		return
	}

	// Parse request
	var req graph.CreateRelationshipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Type == "" {
		h.error(w, "relationship type is required", http.StatusBadRequest)
		return
	}

	// Create relationship
	rel, err := h.neo4j.CreateRelationship(ctx, tenantID,
		req.SourceID.String(),
		req.TargetID.String(),
		req.Type,
		req.Properties,
	)
	if err != nil {
		h.logger.Error("failed to create relationship", zap.Error(err))
		h.error(w, "failed to create relationship", http.StatusInternalServerError)
		return
	}

	// Publish event
	event := &graph.GraphEvent{
		EventType:    "relationship_created",
		TenantID:     tenantID,
		Relationship: rel,
		Timestamp:    time.Now(),
	}
	if err := h.producer.PublishGraphEvent(ctx, event); err != nil {
		h.logger.Error("failed to publish event", zap.Error(err))
	}

	h.json(w, rel, http.StatusCreated)
}

// FindPaths finds paths between nodes
func (h *Handler) FindPaths(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		h.error(w, "missing tenant ID", http.StatusBadRequest)
		return
	}

	// Parse request
	var req graph.PathQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Find paths
	result, err := h.neo4j.FindPaths(ctx, tenantID, &req)
	if err != nil {
		h.logger.Error("failed to find paths", zap.Error(err))
		h.error(w, "failed to find paths", http.StatusInternalServerError)
		return
	}

	h.json(w, result, http.StatusOK)
}

// GetNeighbors gets neighboring nodes
func (h *Handler) GetNeighbors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		h.error(w, "missing tenant ID", http.StatusBadRequest)
		return
	}

	// Parse request
	var req graph.NeighborQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Get neighbors
	result, err := h.neo4j.GetNeighbors(ctx, tenantID, &req)
	if err != nil {
		h.logger.Error("failed to get neighbors", zap.Error(err))
		h.error(w, "failed to get neighbors", http.StatusInternalServerError)
		return
	}

	h.json(w, result, http.StatusOK)
}

// GetStatistics gets graph statistics
func (h *Handler) GetStatistics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		h.error(w, "missing tenant ID", http.StatusBadRequest)
		return
	}

	// Get statistics
	stats, err := h.neo4j.GetStatistics(ctx, tenantID)
	if err != nil {
		h.logger.Error("failed to get statistics", zap.Error(err))
		h.error(w, "failed to get statistics", http.StatusInternalServerError)
		return
	}

	h.json(w, stats, http.StatusOK)
}

// ImportData imports graph data
func (h *Handler) ImportData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		h.error(w, "missing tenant ID", http.StatusBadRequest)
		return
	}

	// Parse request
	var req graph.ImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Import nodes
	nodeMap := make(map[string]string) // Map old IDs to new IDs
	for _, node := range req.Nodes {
		newNode, err := h.neo4j.CreateNode(ctx, tenantID, node.Labels, node.Properties)
		if err != nil {
			h.logger.Error("failed to import node", zap.Error(err))
			continue
		}
		nodeMap[node.ID] = newNode.ID
	}

	// Import relationships
	for _, rel := range req.Relationships {
		startID, ok := nodeMap[rel.StartNode]
		if !ok {
			continue
		}
		endID, ok := nodeMap[rel.EndNode]
		if !ok {
			continue
		}

		_, err := h.neo4j.CreateRelationship(ctx, tenantID, startID, endID, rel.Type, rel.Properties)
		if err != nil {
			h.logger.Error("failed to import relationship", zap.Error(err))
			continue
		}
	}

	response := map[string]interface{}{
		"nodesImported": len(nodeMap),
		"message":       "Import completed",
	}

	h.json(w, response, http.StatusOK)
}

// ExportData exports graph data
func (h *Handler) ExportData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		h.error(w, "missing tenant ID", http.StatusBadRequest)
		return
	}

	// Parse request
	var req graph.ExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Build export query
	nodeQuery := "MATCH (n:Tenant {tenantId: $tenantId})"
	if len(req.NodeLabels) > 0 {
		labelFilter := ""
		for i, label := range req.NodeLabels {
			if i > 0 {
				labelFilter += " OR "
			}
			labelFilter += fmt.Sprintf("n:%s", label)
		}
		nodeQuery += fmt.Sprintf(" WHERE %s", labelFilter)
	}
	nodeQuery += " RETURN n"
	
	if req.Limit > 0 {
		nodeQuery += fmt.Sprintf(" LIMIT %d", req.Limit)
	}

	// Execute query
	result, err := h.neo4j.ExecuteQuery(ctx, tenantID, nodeQuery, nil)
	if err != nil {
		h.logger.Error("failed to export data", zap.Error(err))
		h.error(w, "failed to export data", http.StatusInternalServerError)
		return
	}

	// Build export response
	export := &graph.ImportRequest{
		Nodes:         make([]*graph.ImportNode, 0),
		Relationships: make([]*graph.ImportRelationship, 0),
	}

	for _, node := range result.Nodes {
		export.Nodes = append(export.Nodes, &graph.ImportNode{
			ID:         node.ID,
			Labels:     node.Labels,
			Properties: node.Properties,
		})
	}

	for _, rel := range result.Edges {
		export.Relationships = append(export.Relationships, &graph.ImportRelationship{
			Type:       rel.Type,
			StartNode:  rel.StartNode,
			EndNode:    rel.EndNode,
			Properties: rel.Properties,
		})
	}

	h.json(w, export, http.StatusOK)
}

func (h *Handler) json(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
	}
}

func (h *Handler) error(w http.ResponseWriter, message string, status int) {
	response := map[string]interface{}{
		"error": message,
		"code":  status,
	}
	h.json(w, response, status)
}