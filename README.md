# Knowledge Graph Service

The Knowledge Graph Service manages the graph database (Neo4j) for MemQL, enabling complex relationship queries and knowledge graph operations.

## Features

- **Cypher Query Execution**: Execute arbitrary Cypher queries with tenant isolation
- **Relationship Management**: Create and manage relationships between entities
- **Path Finding**: Find paths between nodes with configurable depth and filters
- **Neighbor Discovery**: Get neighboring nodes with depth and relationship type filters
- **Graph Statistics**: Get statistics about the graph structure
- **Import/Export**: Bulk import and export of graph data
- **Event Processing**: Consume events from other services to update the graph
- **Multi-tenant Isolation**: Complete data isolation between tenants

## Architecture

The service is built with:
- **Neo4j**: Graph database for storing nodes and relationships
- **Kafka**: Event-driven updates from Memory and Entity services
- **Go**: High-performance service implementation
- **Chi Router**: HTTP routing with middleware support

## API Endpoints

### Query Graph
```
POST /api/v1/knowledge-graph/query
```
Execute a Cypher query against the tenant's graph.

### Create Relationship
```
POST /api/v1/knowledge-graph/relationships
```
Create a relationship between two nodes.

### Find Paths
```
POST /api/v1/knowledge-graph/paths
```
Find paths between two nodes with optional constraints.

### Get Neighbors
```
POST /api/v1/knowledge-graph/neighbors
```
Get neighboring nodes of a specific node.

### Get Statistics
```
GET /api/v1/knowledge-graph/statistics
```
Get statistics about the graph (node count, relationship count, etc.).

### Import Data
```
POST /api/v1/knowledge-graph/import
```
Bulk import nodes and relationships.

### Export Data
```
POST /api/v1/knowledge-graph/export
```
Export graph data with optional filters.

## Event Consumers

The service consumes events from:
- **Memory Service**: Creates memory nodes and relationships
- **Entity Service**: Creates entity nodes and mention relationships

## Development

### Prerequisites
- Go 1.21+
- Neo4j 5.0+
- Kafka (for event processing)
- Docker (for local development)

### Running Locally

1. Start Neo4j:
```bash
make neo4j-local
```

2. Install dependencies:
```bash
go mod download
```

3. Run the service:
```bash
make run
```

### Running Tests
```bash
make test
```

### Building Docker Image
```bash
make docker-build
```

## Configuration

Configuration is managed through `config/config.yaml` and environment variables:

- `GRAPH_SERVER_ADDRESS`: HTTP server address (default: `:8084`)
- `GRAPH_NEO4J_URI`: Neo4j connection URI
- `GRAPH_NEO4J_USERNAME`: Neo4j username
- `GRAPH_NEO4J_PASSWORD`: Neo4j password
- `GRAPH_KAFKA_BROKERS`: Kafka broker addresses
- `GRAPH_KAFKA_CONSUMER_GROUP`: Kafka consumer group ID

## Monitoring

The service exposes Prometheus metrics at `/metrics` including:
- Query execution times
- Node and relationship creation rates
- Event processing lag
- Neo4j connection pool metrics

## Security

- All queries are executed with tenant isolation
- Cypher injection prevention through parameterized queries
- JWT authentication required for all API endpoints
- TLS encryption for Neo4j connections in production