package ai

import "testing"

// 自报分类的白名单在 provider 里独立维护（schema enum 与 prompt 各一份），
// 手册扩格时这三处要一起动：这里钉住新格，防 provider 单边漏。
func TestValidateDailyContentAcceptsExpandedCategories(t *testing.T) {
	cases := map[string]string{
		"hair":      `{"topic":"分界线决定视觉重心","lead":"换一条分界线，重心就换了。","fit":"先从现在的分界线往侧边移一指。","why":"分界线改变脸上的视觉重心。","category":"hair","visual":{"modality":"diagram","kind":"body","alt":"中分与侧分的重心示意","items":[{"label":"中分"},{"label":"侧分","state":"pick"}]}}`,
		"makeup":    `{"topic":"妆容只放一个重点","lead":"眼妆和唇色，今天选一处。","fit":"眼妆加重时唇色收中性，两处都重会互相抢。","why":"一个焦点才成立，两个强点会抵消。","category":"makeup","visual":{"modality":"swatch","alt":"眼妆与唇色二选一","items":[{"tone":"#8E9A83","label":"眼妆","state":"pick"},{"tone":"#C9CFD4","label":"唇色","state":"drop"}]}}`,
		"accessory": `{"topic":"鞋与下装的颜色连续","lead":"视线落地不断线，比例就顺。","fit":"鞋的颜色往裤色上靠，最省力的一种连续。","why":"鞋是全身唯一承重的单品，颜色一跳逻辑重排。","category":"accessory","visual":{"modality":"compare","alt":"同色延续与跳色对比","left":{"label":"鞋与下装同色","tone":"#8E9A83"},"right":{"label":"鞋色跳开","tone":"#5A6156"}}}`,
	}
	for category, payload := range cases {
		if err := validateDailyContentPayload([]byte(payload)); err != nil {
			t.Fatalf("%s should be valid: %v", category, err)
		}
	}
}

func TestValidateDailyContentStillRejectsUnknownCategory(t *testing.T) {
	payload := `{"topic":"一个主题","lead":"一句导语。","fit":"一句适配。","why":"一句原理。","category":"lifestyle","visual":{"modality":"swatch","alt":"色卡","items":[{"tone":"#8E9A83","label":"一"},{"tone":"#C9CFD4","label":"二"}]}}`
	if err := validateDailyContentPayload([]byte(payload)); err == nil {
		t.Fatal("unknown category should be rejected")
	}
}
