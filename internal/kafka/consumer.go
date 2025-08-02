package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/memql/graph-service/internal/graph"
	"github.com/memql/graph-service/internal/neo4j"
)

// Consumer handles consuming events from Kafka
type Consumer struct {
	consumer *kafka.Consumer
	neo4j    *neo4j.Client
	producer *Producer
	logger   *zap.Logger
}

// NewConsumer creates a new Kafka consumer
func NewConsumer(
	brokers string,
	groupID string,
	neo4jClient *neo4j.Client,
	producer *Producer,
	logger *zap.Logger,
) (*Consumer, error) {
	config := &kafka.ConfigMap{
		"bootstrap.servers":        brokers,
		"group.id":                 groupID,
		"auto.offset.reset":        "earliest",
		"enable.auto.commit":       false,
		"enable.auto.offset.store": false,
		"session.timeout.ms":       6000,
		"max.poll.interval.ms":     300000,
	}

	consumer, err := kafka.NewConsumer(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	return &Consumer{
		consumer: consumer,
		neo4j:    neo4jClient,
		producer: producer,
		logger:   logger,
	}, nil
}

// Start starts consuming messages
func (c *Consumer) Start(ctx context.Context, topics []string) error {
	if err := c.consumer.SubscribeTopics(topics, nil); err != nil {
		return fmt.Errorf("failed to subscribe to topics: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			msg, err := c.consumer.ReadMessage(100)
			if err != nil {
				if err.(kafka.Error).Code() == kafka.ErrTimedOut {
					continue
				}
				c.logger.Error("failed to read message", zap.Error(err))
				continue
			}

			if err := c.processMessage(ctx, msg); err != nil {
				c.logger.Error("failed to process message",
					zap.String("topic", *msg.TopicPartition.Topic),
					zap.Int32("partition", msg.TopicPartition.Partition),
					zap.Int64("offset", int64(msg.TopicPartition.Offset)),
					zap.Error(err),
				)
				// Continue processing other messages
			}

			// Commit offset
			if _, err := c.consumer.CommitMessage(msg); err != nil {
				c.logger.Error("failed to commit message", zap.Error(err))
			}
		}
	}
}

// Stop stops the consumer
func (c *Consumer) Stop() error {
	return c.consumer.Close()
}

func (c *Consumer) processMessage(ctx context.Context, msg *kafka.Message) error {
	// Extract tenant ID from topic name
	topic := *msg.TopicPartition.Topic
	tenantID := extractTenantID(topic)
	if tenantID == "" {
		return fmt.Errorf("failed to extract tenant ID from topic: %s", topic)
	}

	// Determine event type
	if isEntityTopic(topic) {
		return c.processEntityEvent(ctx, tenantID, msg.Value)
	} else if isMemoryTopic(topic) {
		return c.processMemoryEvent(ctx, tenantID, msg.Value)
	}

	return nil
}

func (c *Consumer) processEntityEvent(ctx context.Context, tenantID string, data []byte) error {
	var event EntityEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("failed to unmarshal entity event: %w", err)
	}

	switch event.EventType {
	case "created":
		return c.handleEntityCreated(ctx, tenantID, &event)
	case "mentioned":
		return c.handleEntityMentioned(ctx, tenantID, &event)
	case "merged":
		return c.handleEntityMerged(ctx, tenantID, &event)
	}

	return nil
}

func (c *Consumer) processMemoryEvent(ctx context.Context, tenantID string, data []byte) error {
	var event MemoryEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("failed to unmarshal memory event: %w", err)
	}

	if event.EventType != "created" {
		return nil
	}

	// Create or update memory node
	properties := map[string]interface{}{
		"id":        event.Memory.ID.String(),
		"agentId":   event.Memory.AgentID.String(),
		"content":   event.Memory.Content,
		"createdAt": event.Memory.CreatedAt.Unix(),
	}

	if event.Memory.SessionID != nil {
		properties["sessionId"] = event.Memory.SessionID.String()
	}

	_, err := c.neo4j.CreateNode(ctx, tenantID, []string{"Memory"}, properties)
	if err != nil {
		return fmt.Errorf("failed to create memory node: %w", err)
	}

	// Create relationship to agent
	_, err = c.neo4j.CreateRelationship(ctx, tenantID,
		event.Memory.ID.String(),
		event.Memory.AgentID.String(),
		"CREATED_BY",
		map[string]interface{}{
			"timestamp": event.Memory.CreatedAt.Unix(),
		},
	)
	if err != nil {
		c.logger.Warn("failed to create agent relationship", zap.Error(err))
	}

	// Publish graph event
	graphEvent := &graph.GraphEvent{
		EventType: "node_created",
		TenantID:  tenantID,
		Node: &graph.Node{
			ID:         event.Memory.ID.String(),
			Labels:     []string{"Memory"},
			Properties: properties,
		},
		Timestamp: time.Now(),
	}

	return c.producer.PublishGraphEvent(ctx, graphEvent)
}

func (c *Consumer) handleEntityCreated(ctx context.Context, tenantID string, event *EntityEvent) error {
	// Create entity node
	properties := map[string]interface{}{
		"id":          event.Entity.ID.String(),
		"name":        event.Entity.Name,
		"entityType":  event.Entity.Type,
		"firstSeen":   event.Entity.FirstSeen.Unix(),
		"lastSeen":    event.Entity.LastSeen.Unix(),
		"occurrences": event.Entity.Occurrences,
	}

	// Add custom properties
	for k, v := range event.Entity.Properties {
		properties[k] = v
	}

	node, err := c.neo4j.CreateNode(ctx, tenantID, []string{"Entity", string(event.Entity.Type)}, properties)
	if err != nil {
		return fmt.Errorf("failed to create entity node: %w", err)
	}

	// Publish graph event
	graphEvent := &graph.GraphEvent{
		EventType: "node_created",
		TenantID:  tenantID,
		Node:      node,
		Timestamp: time.Now(),
	}

	return c.producer.PublishGraphEvent(ctx, graphEvent)
}

func (c *Consumer) handleEntityMentioned(ctx context.Context, tenantID string, event *EntityEvent) error {
	mention := event.Mention
	if mention == nil {
		return fmt.Errorf("mention is nil")
	}

	// Create relationship between memory and entity
	properties := map[string]interface{}{
		"startPos":   mention.Start,
		"endPos":     mention.End,
		"text":       mention.Text,
		"confidence": mention.Confidence,
		"createdAt":  mention.CreatedAt.Unix(),
	}

	rel, err := c.neo4j.CreateRelationship(ctx, tenantID,
		mention.MemoryID.String(),
		mention.EntityID.String(),
		"MENTIONS",
		properties,
	)
	if err != nil {
		// Node might not exist yet, skip
		c.logger.Warn("failed to create mention relationship", 
			zap.String("memory", mention.MemoryID.String()),
			zap.String("entity", mention.EntityID.String()),
			zap.Error(err),
		)
		return nil
	}

	// Publish graph event
	graphEvent := &graph.GraphEvent{
		EventType:    "relationship_created",
		TenantID:     tenantID,
		Relationship: rel,
		Timestamp:    time.Now(),
	}

	return c.producer.PublishGraphEvent(ctx, graphEvent)
}

func (c *Consumer) handleEntityMerged(ctx context.Context, tenantID string, event *EntityEvent) error {
	// Entity merge events would update the graph structure
	// This is a complex operation that might involve:
	// 1. Merging node properties
	// 2. Redirecting relationships
	// 3. Updating related nodes
	
	// For now, just log
	c.logger.Info("entity merged event received",
		zap.String("tenant", tenantID),
		zap.String("entity", event.Entity.ID.String()),
	)
	
	return nil
}

// Helper functions

func extractTenantID(topic string) string {
	// Topic format: tenant-{id}-{type}
	if len(topic) > 7 && topic[:7] == "tenant-" {
		remaining := topic[7:]
		for i, ch := range remaining {
			if ch == '-' {
				return remaining[:i]
			}
		}
	}
	return ""
}

func isEntityTopic(topic string) bool {
	return len(topic) > 9 && topic[len(topic)-9:] == "-entities"
}

func isMemoryTopic(topic string) bool {
	return len(topic) > 9 && topic[len(topic)-9:] == "-memories"
}

// Event types from other services

type EntityEvent struct {
	EventType string    `json:"eventType"`
	TenantID  string    `json:"tenantId"`
	Entity    *Entity   `json:"entity,omitempty"`
	Mention   *Mention  `json:"mention,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type Entity struct {
	ID          uuid.UUID              `json:"id"`
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Properties  map[string]interface{} `json:"properties"`
	FirstSeen   time.Time              `json:"firstSeen"`
	LastSeen    time.Time              `json:"lastSeen"`
	Occurrences int64                  `json:"occurrences"`
}

type Mention struct {
	EntityID   uuid.UUID `json:"entityId"`
	MemoryID   uuid.UUID `json:"memoryId"`
	AgentID    uuid.UUID `json:"agentId"`
	Start      int       `json:"start"`
	End        int       `json:"end"`
	Text       string    `json:"text"`
	Confidence float32   `json:"confidence"`
	CreatedAt  time.Time `json:"createdAt"`
}

type MemoryEvent struct {
	EventType string `json:"eventType"`
	TenantID  string `json:"tenantId"`
	Memory    struct {
		ID        uuid.UUID  `json:"id"`
		AgentID   uuid.UUID  `json:"agentId"`
		SessionID *uuid.UUID `json:"sessionId,omitempty"`
		Content   string     `json:"content"`
		CreatedAt time.Time  `json:"timestamp"`
	} `json:"memory"`
}