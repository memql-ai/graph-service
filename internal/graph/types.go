package graph

import (
	"time"

	"github.com/google/uuid"
)

// Node represents a node in the knowledge graph
type Node struct {
	ID         string                 `json:"id"`
	Labels     []string               `json:"labels"`
	Properties map[string]interface{} `json:"properties"`
}

// Relationship represents an edge in the knowledge graph
type Relationship struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	StartNode  string                 `json:"startNode"`
	EndNode    string                 `json:"endNode"`
	Properties map[string]interface{} `json:"properties"`
}

// GraphQueryRequest represents a request to query the graph
type GraphQueryRequest struct {
	Query      string                 `json:"query" validate:"required"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// GraphQueryResponse represents a query response
type GraphQueryResponse struct {
	Nodes    []*Node         `json:"nodes"`
	Edges    []*Relationship `json:"edges"`
	Records  []interface{}   `json:"records,omitempty"`
	Duration int64           `json:"duration"` // Query duration in milliseconds
}

// CreateRelationshipRequest represents a request to create a relationship
type CreateRelationshipRequest struct {
	SourceID   uuid.UUID              `json:"sourceId" validate:"required"`
	TargetID   uuid.UUID              `json:"targetId" validate:"required"`
	Type       string                 `json:"type" validate:"required"`
	Properties map[string]interface{} `json:"properties,omitempty"`
}

// PathQueryRequest represents a request to find paths between nodes
type PathQueryRequest struct {
	StartNodeID uuid.UUID `json:"startNodeId" validate:"required"`
	EndNodeID   uuid.UUID `json:"endNodeId" validate:"required"`
	MaxDepth    int       `json:"maxDepth,omitempty"`
	RelTypes    []string  `json:"relTypes,omitempty"`
}

// PathQueryResponse represents paths found between nodes
type PathQueryResponse struct {
	Paths []Path `json:"paths"`
}

// Path represents a path through the graph
type Path struct {
	Nodes         []*Node         `json:"nodes"`
	Relationships []*Relationship `json:"relationships"`
	Length        int             `json:"length"`
}

// NeighborQueryRequest represents a request to find neighbors
type NeighborQueryRequest struct {
	NodeID   uuid.UUID `json:"nodeId" validate:"required"`
	Depth    int       `json:"depth,omitempty"`
	RelTypes []string  `json:"relTypes,omitempty"`
	Limit    int       `json:"limit,omitempty"`
}

// NeighborQueryResponse represents neighbors found
type NeighborQueryResponse struct {
	Nodes         []*Node         `json:"nodes"`
	Relationships []*Relationship `json:"relationships"`
}

// GraphStatistics represents statistics about the graph
type GraphStatistics struct {
	NodeCount         int64            `json:"nodeCount"`
	RelationshipCount int64            `json:"relationshipCount"`
	NodesByLabel      map[string]int64 `json:"nodesByLabel"`
	RelationshipsByType map[string]int64 `json:"relationshipsByType"`
}

// ImportRequest represents a request to import graph data
type ImportRequest struct {
	Nodes         []*ImportNode         `json:"nodes"`
	Relationships []*ImportRelationship `json:"relationships"`
}

// ImportNode represents a node to import
type ImportNode struct {
	ID         string                 `json:"id"`
	Labels     []string               `json:"labels"`
	Properties map[string]interface{} `json:"properties"`
}

// ImportRelationship represents a relationship to import
type ImportRelationship struct {
	Type       string                 `json:"type"`
	StartNode  string                 `json:"startNode"`
	EndNode    string                 `json:"endNode"`
	Properties map[string]interface{} `json:"properties"`
}

// ExportRequest represents a request to export graph data
type ExportRequest struct {
	NodeLabels []string `json:"nodeLabels,omitempty"`
	RelTypes   []string `json:"relTypes,omitempty"`
	Limit      int      `json:"limit,omitempty"`
}

// GraphEvent represents an event for Kafka
type GraphEvent struct {
	EventType    string        `json:"eventType"` // node_created, node_updated, relationship_created, etc.
	TenantID     string        `json:"tenantId"`
	Node         *Node         `json:"node,omitempty"`
	Relationship *Relationship `json:"relationship,omitempty"`
	Timestamp    time.Time     `json:"timestamp"`
}

// CypherTemplate represents a saved Cypher query template
type CypherTemplate struct {
	ID          uuid.UUID   `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Query       string      `json:"query"`
	Parameters  []Parameter `json:"parameters"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
}

// Parameter represents a query parameter
type Parameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}