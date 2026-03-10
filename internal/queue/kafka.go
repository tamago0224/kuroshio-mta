package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/tamago0224/orinoco-mta/internal/config"
	"github.com/tamago0224/orinoco-mta/internal/model"
)

type kafkaReceipt struct {
	source string
	msg    kafka.Message
}

type stagedKafkaMessage struct {
	source string
	msg    kafka.Message
	data   *model.Message
}

type KafkaStore struct {
	inboundReader *kafka.Reader
	retryReader   *kafka.Reader
	writers       map[string]*kafka.Writer
	topics        kafkaTopics

	mu       sync.Mutex
	staged   []stagedKafkaMessage
	receipts map[string]kafkaReceipt
}

type kafkaTopics struct {
	inbound string
	retry   string
	dlq     string
	sent    string
}

func NewKafka(cfg config.Config) (*KafkaStore, error) {
	brokers := make([]string, 0, len(cfg.KafkaBrokers))
	for _, b := range cfg.KafkaBrokers {
		v := strings.TrimSpace(b)
		if v != "" {
			brokers = append(brokers, v)
		}
	}
	if len(brokers) == 0 {
		return nil, errors.New("kafka backend requires at least one broker")
	}
	groupID := strings.TrimSpace(cfg.KafkaConsumerGroup)
	if groupID == "" {
		groupID = "orinoco-mta"
	}
	topics := kafkaTopics{
		inbound: strings.TrimSpace(cfg.KafkaTopicInbound),
		retry:   strings.TrimSpace(cfg.KafkaTopicRetry),
		dlq:     strings.TrimSpace(cfg.KafkaTopicDLQ),
		sent:    strings.TrimSpace(cfg.KafkaTopicSent),
	}
	if topics.inbound == "" || topics.retry == "" || topics.dlq == "" || topics.sent == "" {
		return nil, errors.New("kafka topics must be configured")
	}

	k := &KafkaStore{
		topics: topics,
		writers: map[string]*kafka.Writer{
			topics.inbound: newKafkaWriter(brokers, topics.inbound),
			topics.retry:   newKafkaWriter(brokers, topics.retry),
			topics.dlq:     newKafkaWriter(brokers, topics.dlq),
			topics.sent:    newKafkaWriter(brokers, topics.sent),
		},
		inboundReader: newKafkaReader(brokers, groupID, topics.inbound),
		retryReader:   newKafkaReader(brokers, groupID, topics.retry),
		receipts:      map[string]kafkaReceipt{},
	}
	return k, nil
}

func newKafkaReader(brokers []string, groupID, topic string) *kafka.Reader {
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		GroupID:        groupID,
		Topic:          topic,
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: 0,
		StartOffset:    kafka.FirstOffset,
	})
}

func newKafkaWriter(brokers []string, topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireAll,
		Async:        false,
	}
}

func (k *KafkaStore) Enqueue(msg *model.Message) error {
	now := time.Now().UTC()
	msg.CreatedAt = now
	msg.UpdatedAt = now
	msg.NextAttempt = now
	return k.publish(k.topics.inbound, msg)
}

func (k *KafkaStore) Due(limit int) ([]*model.Message, error) {
	if limit <= 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	out := make([]*model.Message, 0, limit)
	seen := make(map[string]struct{}, limit)

	// Prefer already-fetched messages first.
	k.mu.Lock()
	rest := k.staged[:0]
	for _, item := range k.staged {
		if len(out) >= limit {
			rest = append(rest, item)
			continue
		}
		if item.data.NextAttempt.After(now) {
			rest = append(rest, item)
			continue
		}
		if _, dup := seen[item.data.ID]; dup {
			// Drop duplicate message-id to keep processing idempotent.
			_ = k.commitByReceipt(item.source, item.msg)
			continue
		}
		k.receipts[item.data.ID] = kafkaReceipt{source: item.source, msg: item.msg}
		seen[item.data.ID] = struct{}{}
		out = append(out, item.data)
	}
	k.staged = rest
	k.mu.Unlock()
	if len(out) >= limit {
		return out, nil
	}

	remaining := limit - len(out)
	for _, src := range []string{"retry", "inbound"} {
		for i := 0; i < remaining*2; i++ {
			item, ok, err := k.fetchOne(src)
			if err != nil {
				return out, err
			}
			if !ok {
				break
			}
			if item.data.NextAttempt.After(now) {
				k.mu.Lock()
				k.staged = append(k.staged, item)
				k.mu.Unlock()
				continue
			}
			if _, dup := seen[item.data.ID]; dup {
				_ = k.commitByReceipt(item.source, item.msg)
				continue
			}
			k.mu.Lock()
			k.receipts[item.data.ID] = kafkaReceipt{source: item.source, msg: item.msg}
			k.mu.Unlock()
			seen[item.data.ID] = struct{}{}
			out = append(out, item.data)
			if len(out) >= limit {
				return out, nil
			}
		}
		remaining = limit - len(out)
		if remaining <= 0 {
			break
		}
	}
	return out, nil
}

func (k *KafkaStore) commitByReceipt(source string, msg kafka.Message) error {
	var reader *kafka.Reader
	switch source {
	case "retry":
		reader = k.retryReader
	case "inbound":
		reader = k.inboundReader
	default:
		return errors.New("unknown receipt source: " + source)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return reader.CommitMessages(ctx, msg)
}

func (k *KafkaStore) AckSent(id string, msg *model.Message) error {
	msg.UpdatedAt = time.Now().UTC()
	if err := k.publish(k.topics.sent, msg); err != nil {
		return err
	}
	return k.commitByID(id)
}

func (k *KafkaStore) Retry(msg *model.Message, delay time.Duration, reason string) error {
	msg.Attempts++
	msg.LastError = reason
	msg.UpdatedAt = time.Now().UTC()
	msg.NextAttempt = msg.UpdatedAt.Add(delay)
	if err := k.publish(k.topics.retry, msg); err != nil {
		return err
	}
	return k.commitByID(msg.ID)
}

func (k *KafkaStore) Fail(msg *model.Message, reason string) error {
	msg.Attempts++
	msg.LastError = reason
	msg.UpdatedAt = time.Now().UTC()
	if err := k.publish(k.topics.dlq, msg); err != nil {
		return err
	}
	return k.commitByID(msg.ID)
}

func (k *KafkaStore) Close() error {
	var errs []error
	if k.inboundReader != nil {
		if err := k.inboundReader.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if k.retryReader != nil {
		if err := k.retryReader.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	for _, w := range k.writers {
		if err := w.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (k *KafkaStore) fetchOne(source string) (stagedKafkaMessage, bool, error) {
	var reader *kafka.Reader
	switch source {
	case "retry":
		reader = k.retryReader
	case "inbound":
		reader = k.inboundReader
	default:
		return stagedKafkaMessage{}, false, fmt.Errorf("unknown source: %s", source)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	m, err := reader.FetchMessage(ctx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return stagedKafkaMessage{}, false, nil
		}
		return stagedKafkaMessage{}, false, err
	}
	msg, err := unmarshalMessage(m.Value)
	if err != nil {
		// Commit poison message to avoid hot-loop.
		_ = reader.CommitMessages(context.Background(), m)
		return stagedKafkaMessage{}, false, nil
	}
	return stagedKafkaMessage{source: source, msg: m, data: msg}, true, nil
}

func (k *KafkaStore) publish(topic string, msg *model.Message) error {
	w, ok := k.writers[topic]
	if !ok {
		return errors.New("writer not found for topic: " + topic)
	}
	b, err := marshalMessage(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return w.WriteMessages(ctx, kafka.Message{
		Key:   []byte(msg.ID),
		Value: b,
		Time:  time.Now().UTC(),
	})
}

func (k *KafkaStore) commitByID(id string) error {
	k.mu.Lock()
	receipt, ok := k.receipts[id]
	if ok {
		delete(k.receipts, id)
	}
	k.mu.Unlock()
	if !ok {
		return nil
	}
	return k.commitByReceipt(receipt.source, receipt.msg)
}

func marshalMessage(msg *model.Message) ([]byte, error) {
	return json.Marshal(msg)
}

func unmarshalMessage(b []byte) (*model.Message, error) {
	var msg model.Message
	if err := json.Unmarshal(b, &msg); err != nil {
		return nil, err
	}
	if strings.TrimSpace(msg.ID) == "" {
		return nil, errors.New("message id is required")
	}
	return &msg, nil
}
