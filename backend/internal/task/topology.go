package task

import (
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// 拓扑默认命名（01 §7.1 权威；自定义 main exchange 时 EnsureTopology 以参数为准）。
const (
	DefaultExchange = "cloudfog.tasks" // direct · durable：路由键 = 任务类型
	exchangeDLX     = "cloudfog.dlx"   // fanout · durable：业务队列死信
	queueDLQ        = "task.dlq"       // durable

	exchangeRetryPrefix = "cloudfog.retry." // fanout ×N（每档一个），桶队列 ingress
	exchangeDelayPrefix = "cloudfog.delay." // fanout ×N（每档一个），EnqueueIn 用
	queueRetryPrefix    = "retry."          // retry.5s / retry.1m / retry.5m
	queueDelayPrefix    = "delay."          // delay.5m / delay.15m / delay.30m
)

// BusinessQueue 返回逻辑队列对应的 broker 队列名。
func BusinessQueue(q QueueName) (string, error) {
	switch q {
	case QueueCritical:
		return "task.critical", nil
	case QueueDefault:
		return "task.default", nil
	case QueueLow:
		return "task.low", nil
	default:
		return "", fmt.Errorf("task: 未知逻辑队列 %q", q)
	}
}

// delayMS 时长转毫秒（TTL 参数，int32 上限保护）。
func delayMS(d time.Duration) int32 {
	ms := d.Milliseconds()
	if ms <= 0 {
		ms = 1
	}
	if ms > int64(^uint32(0)>>1) {
		return int32(^uint32(0) >> 1)
	}
	return int32(ms)
}

// EnsureTopology 幂等声明全部拓扑（exchange/队列/binding/死信）。
// mainExchange = 主交换机名（发布与 TTL 桶 DLX 目标一致；RabbitEnqueuer 以 config 传入）。
// 可重复调用（broker 声明均幂等）；consumer 启动前与发布前各执行一次。
func EnsureTopology(ch *amqp.Channel, mainExchange string, reg *Registry, retryBuckets, delayBuckets []time.Duration) error {
	if ch == nil {
		return fmt.Errorf("task: EnsureTopology 需要非 nil channel")
	}
	if mainExchange == "" {
		mainExchange = DefaultExchange
	}
	// 1) 主交换机 + DLX/DLQ
	if err := ch.ExchangeDeclare(mainExchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("task: 声明 %s 失败: %w", mainExchange, err)
	}
	if err := ch.ExchangeDeclare(exchangeDLX, "fanout", true, false, false, false, nil); err != nil {
		return fmt.Errorf("task: 声明 %s 失败: %w", exchangeDLX, err)
	}
	if _, err := ch.QueueDeclare(queueDLQ, true, false, false, false, nil); err != nil {
		return fmt.Errorf("task: 声明 %s 失败: %w", queueDLQ, err)
	}
	if err := ch.QueueBind(queueDLQ, "", exchangeDLX, false, nil); err != nil {
		return fmt.Errorf("task: 绑定 %s→%s 失败: %w", queueDLQ, exchangeDLX, err)
	}

	// 2) 业务三队列（durable classic；DLX = cloudfog.dlx）按注册表逐一 binding
	for _, q := range []QueueName{QueueCritical, QueueDefault, QueueLow} {
		name, err := BusinessQueue(q)
		if err != nil {
			return err
		}
		args := amqp.Table{"x-dead-letter-exchange": exchangeDLX}
		if _, err := ch.QueueDeclare(name, true, false, false, false, args); err != nil {
			return fmt.Errorf("task: 声明 %s 失败: %w", name, err)
		}
		for _, ty := range reg.Types(q) {
			if err := ch.QueueBind(name, string(ty), mainExchange, false, nil); err != nil {
				return fmt.Errorf("task: 绑定 %s ← %q 失败: %w", name, ty, err)
			}
		}
	}

	// 3) 重试/延迟 fanout 桶（TTL + DLX 回 mainExchange；fanout ingress 保留 original routing key）
	if err := declareTTLBuckets(ch, exchangeRetryPrefix, queueRetryPrefix, retryBuckets, mainExchange); err != nil {
		return err
	}
	if err := declareTTLBuckets(ch, exchangeDelayPrefix, queueDelayPrefix, delayBuckets, mainExchange); err != nil {
		return err
	}
	return nil
}

// declareTTLBuckets 为每档时长声明一个 fanout exchange + 一个 TTL 队列并绑定。
// 队列 x-message-ttl = 档位；DLX 回 mainExchange（必须与业务绑定同 exchange，
// 否则 TTL 到期死信发回不存在/错位的交换机 → broker 静默丢弃，资金级丢失）。
func declareTTLBuckets(ch *amqp.Channel, exPrefix, qPrefix string, buckets []time.Duration, mainExchange string) error {
	for _, b := range buckets {
		ex := exPrefix + formatBucket(b)
		q := qPrefix + formatBucket(b)
		if err := ch.ExchangeDeclare(ex, "fanout", true, false, false, false, nil); err != nil {
			return fmt.Errorf("task: 声明 %s 失败: %w", ex, err)
		}
		args := amqp.Table{
			"x-message-ttl":          delayMS(b),
			"x-dead-letter-exchange": mainExchange,
		}
		if _, err := ch.QueueDeclare(q, true, false, false, false, args); err != nil {
			return fmt.Errorf("task: 声明 %s 失败: %w", q, err)
		}
		if err := ch.QueueBind(q, "", ex, false, nil); err != nil {
			return fmt.Errorf("task: 绑定 %s→%s 失败: %w", q, ex, err)
		}
	}
	return nil
}

// formatBucket 生成档位后缀（500ms / 5s / 1m / 90s / 1h…，粒度精确，杜绝截断错位）。
func formatBucket(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	total := int64(d / time.Second)
	switch {
	case total%3600 == 0 && total >= 3600:
		return fmt.Sprintf("%dh", total/3600)
	case total%60 == 0 && total >= 60:
		return fmt.Sprintf("%dm", total/60)
	default:
		return fmt.Sprintf("%ds", total)
	}
}
