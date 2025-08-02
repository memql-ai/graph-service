package kafka

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"go.uber.org/zap"

	"github.com/memql/graph-service/internal/graph"
)

// Producer handles publishing events to Kafka
type Producer struct {
	producer *kafka.Producer
	logger   *zap.Logger
}

// NewProducer creates a new Kafka producer
func NewProducer(brokers string, logger *zap.Logger) (*Producer, error) {
	config := &kafka.ConfigMap{
		"bootstrap.servers": brokers,
		"client.id":         "graph-service",
		"acks":              "all",
		"retries":           10,
		"retry.backoff.ms":  100,
		"compression.type":  "snappy",
		"linger.ms":         10,
		"batch.size":        16384,
	}

	producer, err := kafka.NewProducer(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create producer: %w", err)
	}

	// Start delivery report handler
	go func() {
		for e := range producer.Events() {
			switch ev := e.(type) {
			case *kafka.Message:
				if ev.TopicPartition.Error != nil {
					logger.Error("delivery failed",
						zap.String("topic", *ev.TopicPartition.Topic),
						zap.Error(ev.TopicPartition.Error),
					)
				}
			}
		}
	}()

	return &Producer{
		producer: producer,
		logger:   logger,
	}, nil
}

// PublishGraphEvent publishes a graph event to Kafka
func (p *Producer) PublishGraphEvent(ctx context.Context, event *graph.GraphEvent) error {
	// Get topic name based on tenant
	topic := p.getGraphTopic(event.TenantID)

	// Marshal event to JSON
	value, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Determine key based on event type
	var key string
	if event.Node != nil {
		key = event.Node.ID
	} else if event.Relationship != nil {
		key = event.Relationship.ID
	}

	// Create message
	message := &kafka.Message{
		TopicPartition: kafka.TopicPartition{
			Topic:     &topic,
			Partition: kafka.PartitionAny,
		},
		Key:   []byte(key),
		Value: value,
		Headers: []kafka.Header{
			{Key: "event_type", Value: []byte(event.EventType)},
			{Key: "tenant_id", Value: []byte(event.TenantID)},
		},
	}

	// Send message
	err = p.producer.Produce(message, nil)
	if err != nil {
		return fmt.Errorf("failed to produce message: %w", err)
	}

	return nil
}

// Close closes the producer
func (p *Producer) Close() {
	p.producer.Flush(15000) // Wait up to 15 seconds
	p.producer.Close()
}

func (p *Producer) getGraphTopic(tenantID string) string {
	// Use tenant-specific topic for isolation
	return fmt.Sprintf("tenant-%s-graph", tenantID)
}