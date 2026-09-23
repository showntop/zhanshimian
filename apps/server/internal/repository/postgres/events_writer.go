package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
)

// TrackProductEventRow 是埋点的窄写入（从已删除的 product_loops.go 机械迁出；
// 名称加 Row 后缀避免与旧调用面混淆）。静默失败由调用方负责。
func (s *Store) TrackProductEventRow(ctx context.Context, userID string, input domain.ProductEventInput) error {
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 64 || len(input.Payload) > 4096 {
		return fmt.Errorf("invalid event name or payload")
	}
	payload := input.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO product_events(user_id,name,payload) VALUES($1,$2,$3)`, userID, name, payload)
	return err
}
