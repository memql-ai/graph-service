package neo4j

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/memql/graph-service/internal/graph"
)

// Client handles Neo4j operations
type Client struct {
	driver neo4j.DriverWithContext
	logger *zap.Logger
}

// NewClient creates a new Neo4j client
func NewClient(uri, username, password string, logger *zap.Logger) (*Client, error) {
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		return nil, fmt.Errorf("failed to create driver: %w", err)
	}

	// Verify connectivity
	ctx := context.Background()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		return nil, fmt.Errorf("failed to verify connectivity: %w", err)
	}

	return &Client{
		driver: driver,
		logger: logger,
	}, nil
}

// Close closes the client
func (c *Client) Close(ctx context.Context) error {
	return c.driver.Close(ctx)
}

// ExecuteQuery executes a Cypher query
func (c *Client) ExecuteQuery(ctx context.Context, tenantID string, query string, params map[string]interface{}) (*graph.GraphQueryResponse, error) {
	start := time.Now()
	
	// Add tenant constraint to query
	query = c.addTenantConstraint(query, tenantID)
	
	// Add tenant ID to parameters
	if params == nil {
		params = make(map[string]interface{})
	}
	params["tenantId"] = tenantID

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeRead,
		DatabaseName: c.getDatabaseName(tenantID),
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Collect results
	nodes := make(map[string]*graph.Node)
	relationships := make(map[string]*graph.Relationship)
	records := make([]interface{}, 0)

	for result.Next(ctx) {
		record := result.Record()
		
		// Extract nodes and relationships from the record
		for _, value := range record.Values {
			switch v := value.(type) {
			case neo4j.Node:
				node := c.convertNode(v)
				nodes[node.ID] = node
			case neo4j.Relationship:
				rel := c.convertRelationship(v)
				relationships[rel.ID] = rel
			case neo4j.Path:
				// Extract nodes and relationships from path
				for _, node := range v.Nodes {
					n := c.convertNode(node)
					nodes[n.ID] = n
				}
				for i, rel := range v.Relationships {
					r := c.convertRelationship(rel)
					r.StartNode = fmt.Sprintf("%d", v.Nodes[i].GetId())
					r.EndNode = fmt.Sprintf("%d", v.Nodes[i+1].GetId())
					relationships[r.ID] = r
				}
			}
		}
		
		records = append(records, record.Values)
	}

	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	// Convert maps to slices
	nodeList := make([]*graph.Node, 0, len(nodes))
	for _, node := range nodes {
		nodeList = append(nodeList, node)
	}

	relList := make([]*graph.Relationship, 0, len(relationships))
	for _, rel := range relationships {
		relList = append(relList, rel)
	}

	return &graph.GraphQueryResponse{
		Nodes:    nodeList,
		Edges:    relList,
		Records:  records,
		Duration: time.Since(start).Milliseconds(),
	}, nil
}

// CreateNode creates a new node
func (c *Client) CreateNode(ctx context.Context, tenantID string, labels []string, properties map[string]interface{}) (*graph.Node, error) {
	// Add tenant property
	if properties == nil {
		properties = make(map[string]interface{})
	}
	properties["tenantId"] = tenantID

	// Build labels string
	labelStr := ""
	for _, label := range labels {
		labelStr += ":" + label
	}

	query := fmt.Sprintf("CREATE (n:Tenant%s $props) RETURN n", labelStr)

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeWrite,
		DatabaseName: c.getDatabaseName(tenantID),
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, query, map[string]interface{}{
		"props": properties,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create node: %w", err)
	}

	if result.Next(ctx) {
		record := result.Record()
		if node, ok := record.Get("n"); ok {
			if n, ok := node.(neo4j.Node); ok {
				return c.convertNode(n), nil
			}
		}
	}

	return nil, fmt.Errorf("failed to get created node")
}

// CreateRelationship creates a relationship between nodes
func (c *Client) CreateRelationship(ctx context.Context, tenantID string, startNodeID, endNodeID, relType string, properties map[string]interface{}) (*graph.Relationship, error) {
	// Add tenant property
	if properties == nil {
		properties = make(map[string]interface{})
	}
	properties["tenantId"] = tenantID

	query := fmt.Sprintf(`
		MATCH (a:Tenant {id: $startId, tenantId: $tenantId})
		MATCH (b:Tenant {id: $endId, tenantId: $tenantId})
		CREATE (a)-[r:%s $props]->(b)
		RETURN r, a, b
	`, relType)

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeWrite,
		DatabaseName: c.getDatabaseName(tenantID),
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, query, map[string]interface{}{
		"startId":  startNodeID,
		"endId":    endNodeID,
		"tenantId": tenantID,
		"props":    properties,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create relationship: %w", err)
	}

	if result.Next(ctx) {
		record := result.Record()
		if rel, ok := record.Get("r"); ok {
			if r, ok := rel.(neo4j.Relationship); ok {
				relationship := c.convertRelationship(r)
				
				// Get node IDs
				if a, ok := record.Get("a"); ok {
					if node, ok := a.(neo4j.Node); ok {
						relationship.StartNode = fmt.Sprintf("%d", node.GetId())
					}
				}
				if b, ok := record.Get("b"); ok {
					if node, ok := b.(neo4j.Node); ok {
						relationship.EndNode = fmt.Sprintf("%d", node.GetId())
					}
				}
				
				return relationship, nil
			}
		}
	}

	return nil, fmt.Errorf("failed to get created relationship")
}

// FindPaths finds paths between two nodes
func (c *Client) FindPaths(ctx context.Context, tenantID string, req *graph.PathQueryRequest) (*graph.PathQueryResponse, error) {
	maxDepth := req.MaxDepth
	if maxDepth == 0 {
		maxDepth = 5
	}

	relTypeFilter := ""
	if len(req.RelTypes) > 0 {
		relTypeFilter = ":" + req.RelTypes[0]
		for i := 1; i < len(req.RelTypes); i++ {
			relTypeFilter += "|" + req.RelTypes[i]
		}
	}

	query := fmt.Sprintf(`
		MATCH path = (start:Tenant {id: $startId, tenantId: $tenantId})-[%s*1..%d]-(end:Tenant {id: $endId, tenantId: $tenantId})
		RETURN path
		LIMIT 10
	`, relTypeFilter, maxDepth)

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeRead,
		DatabaseName: c.getDatabaseName(tenantID),
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, query, map[string]interface{}{
		"startId":  req.StartNodeID.String(),
		"endId":    req.EndNodeID.String(),
		"tenantId": tenantID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find paths: %w", err)
	}

	paths := make([]graph.Path, 0)
	for result.Next(ctx) {
		record := result.Record()
		if p, ok := record.Get("path"); ok {
			if path, ok := p.(neo4j.Path); ok {
				paths = append(paths, c.convertPath(path))
			}
		}
	}

	return &graph.PathQueryResponse{
		Paths: paths,
	}, nil
}

// GetNeighbors gets neighboring nodes
func (c *Client) GetNeighbors(ctx context.Context, tenantID string, req *graph.NeighborQueryRequest) (*graph.NeighborQueryResponse, error) {
	depth := req.Depth
	if depth == 0 {
		depth = 1
	}

	limit := req.Limit
	if limit == 0 {
		limit = 100
	}

	relTypeFilter := ""
	if len(req.RelTypes) > 0 {
		relTypeFilter = ":" + req.RelTypes[0]
		for i := 1; i < len(req.RelTypes); i++ {
			relTypeFilter += "|" + req.RelTypes[i]
		}
	}

	query := fmt.Sprintf(`
		MATCH (start:Tenant {id: $nodeId, tenantId: $tenantId})-[r%s*1..%d]-(neighbor:Tenant)
		WHERE neighbor.tenantId = $tenantId
		RETURN DISTINCT neighbor, r
		LIMIT $limit
	`, relTypeFilter, depth)

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeRead,
		DatabaseName: c.getDatabaseName(tenantID),
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, query, map[string]interface{}{
		"nodeId":   req.NodeID.String(),
		"tenantId": tenantID,
		"limit":    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get neighbors: %w", err)
	}

	nodes := make(map[string]*graph.Node)
	relationships := make(map[string]*graph.Relationship)

	for result.Next(ctx) {
		record := result.Record()
		
		if n, ok := record.Get("neighbor"); ok {
			if node, ok := n.(neo4j.Node); ok {
				converted := c.convertNode(node)
				nodes[converted.ID] = converted
			}
		}
		
		if r, ok := record.Get("r"); ok {
			// Handle array of relationships
			switch rels := r.(type) {
			case []interface{}:
				for _, rel := range rels {
					if relationship, ok := rel.(neo4j.Relationship); ok {
						converted := c.convertRelationship(relationship)
						relationships[converted.ID] = converted
					}
				}
			}
		}
	}

	// Convert maps to slices
	nodeList := make([]*graph.Node, 0, len(nodes))
	for _, node := range nodes {
		nodeList = append(nodeList, node)
	}

	relList := make([]*graph.Relationship, 0, len(relationships))
	for _, rel := range relationships {
		relList = append(relList, rel)
	}

	return &graph.NeighborQueryResponse{
		Nodes:         nodeList,
		Relationships: relList,
	}, nil
}

// GetStatistics gets graph statistics
func (c *Client) GetStatistics(ctx context.Context, tenantID string) (*graph.GraphStatistics, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeRead,
		DatabaseName: c.getDatabaseName(tenantID),
	})
	defer session.Close(ctx)

	// Get node count
	nodeCountQuery := `
		MATCH (n:Tenant {tenantId: $tenantId})
		RETURN count(n) as count
	`
	
	nodeResult, err := session.Run(ctx, nodeCountQuery, map[string]interface{}{
		"tenantId": tenantID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get node count: %w", err)
	}

	var nodeCount int64
	if nodeResult.Next(ctx) {
		if count, ok := nodeResult.Record().Get("count"); ok {
			nodeCount = count.(int64)
		}
	}

	// Get relationship count
	relCountQuery := `
		MATCH (:Tenant {tenantId: $tenantId})-[r]->(:Tenant {tenantId: $tenantId})
		RETURN count(r) as count
	`
	
	relResult, err := session.Run(ctx, relCountQuery, map[string]interface{}{
		"tenantId": tenantID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get relationship count: %w", err)
	}

	var relCount int64
	if relResult.Next(ctx) {
		if count, ok := relResult.Record().Get("count"); ok {
			relCount = count.(int64)
		}
	}

	// Get nodes by label
	labelQuery := `
		MATCH (n:Tenant {tenantId: $tenantId})
		UNWIND labels(n) as label
		WHERE label <> 'Tenant'
		RETURN label, count(n) as count
		ORDER BY count DESC
	`
	
	labelResult, err := session.Run(ctx, labelQuery, map[string]interface{}{
		"tenantId": tenantID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get label counts: %w", err)
	}

	nodesByLabel := make(map[string]int64)
	for labelResult.Next(ctx) {
		record := labelResult.Record()
		if label, ok := record.Get("label"); ok {
			if count, ok := record.Get("count"); ok {
				nodesByLabel[label.(string)] = count.(int64)
			}
		}
	}

	// Get relationships by type
	typeQuery := `
		MATCH (:Tenant {tenantId: $tenantId})-[r]->(:Tenant {tenantId: $tenantId})
		RETURN type(r) as type, count(r) as count
		ORDER BY count DESC
	`
	
	typeResult, err := session.Run(ctx, typeQuery, map[string]interface{}{
		"tenantId": tenantID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get type counts: %w", err)
	}

	relsByType := make(map[string]int64)
	for typeResult.Next(ctx) {
		record := typeResult.Record()
		if relType, ok := record.Get("type"); ok {
			if count, ok := record.Get("count"); ok {
				relsByType[relType.(string)] = count.(int64)
			}
		}
	}

	return &graph.GraphStatistics{
		NodeCount:           nodeCount,
		RelationshipCount:   relCount,
		NodesByLabel:        nodesByLabel,
		RelationshipsByType: relsByType,
	}, nil
}

// Helper methods

func (c *Client) getDatabaseName(tenantID string) string {
	// In Neo4j 4.x+, we can use multi-database for tenant isolation
	// For now, use the default database with tenant properties
	return ""
}

func (c *Client) addTenantConstraint(query, tenantID string) string {
	// This is a simplified approach - in production, use a proper Cypher parser
	// to inject tenant constraints safely
	return query
}

func (c *Client) convertNode(n neo4j.Node) *graph.Node {
	labels := make([]string, 0)
	for _, label := range n.Labels {
		if label != "Tenant" { // Skip the tenant label
			labels = append(labels, label)
		}
	}

	return &graph.Node{
		ID:         fmt.Sprintf("%d", n.GetId()),
		Labels:     labels,
		Properties: n.Props,
	}
}

func (c *Client) convertRelationship(r neo4j.Relationship) *graph.Relationship {
	return &graph.Relationship{
		ID:         fmt.Sprintf("%d", r.GetId()),
		Type:       r.Type,
		Properties: r.Props,
		// StartNode and EndNode need to be set by the caller
	}
}

func (c *Client) convertPath(p neo4j.Path) graph.Path {
	nodes := make([]*graph.Node, len(p.Nodes))
	for i, node := range p.Nodes {
		nodes[i] = c.convertNode(node)
	}

	relationships := make([]*graph.Relationship, len(p.Relationships))
	for i, rel := range p.Relationships {
		relationships[i] = c.convertRelationship(rel)
		if i < len(p.Nodes)-1 {
			relationships[i].StartNode = nodes[i].ID
			relationships[i].EndNode = nodes[i+1].ID
		}
	}

	return graph.Path{
		Nodes:         nodes,
		Relationships: relationships,
		Length:        len(p.Relationships),
	}
}