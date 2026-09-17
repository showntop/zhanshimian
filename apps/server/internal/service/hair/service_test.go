// 包内单测（不依赖数据库）：方向描述的归一化与提示词拼装是「用户文本进模型」的
// 唯一入口，边界必须锁在单元测试里；全链路另见 handler_integration_test.go。
package hair

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeDirection(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "空串合法", raw: "   ", want: ""},
		{name: "去首尾空白", raw: "  两侧推短  ", want: "两侧推短"},
		{name: "换行折叠成空格", raw: "两侧推短\n顶部留长", want: "两侧推短 顶部留长"},
		{name: "去掉书名字号引号", raw: "「空气感」微卷", want: "空气感 微卷"},
		{name: "压掉零宽与制表", raw: "短寸\u200b\t纹理", want: "短寸 纹理"},
		{name: "四十字内保留原样", raw: strings.Repeat("发", DirectionMaxRunes), want: strings.Repeat("发", DirectionMaxRunes)},
	}
	for _, tc := range cases {
		got, err := NormalizeDirection(tc.raw)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}

	if _, err := NormalizeDirection(strings.Repeat("发", DirectionMaxRunes+1)); !errors.Is(err, ErrDirectionInvalid) {
		t.Fatalf("超长描述应报 ErrDirectionInvalid，got %v", err)
	}
}

func TestPreviewPromptUsesDirection(t *testing.T) {
	prompt := previewPrompt(PreviewWork{StyleID: CustomDirectionID, StyleName: "两侧推短，顶部留一点长度"})
	if !strings.Contains(prompt, "「两侧推短，顶部留一点长度」") {
		t.Fatalf("自定义方向必须进提示词: %q", prompt)
	}
	// 红线 1：只改发型，不打分不评判五官/身材
	if !strings.Contains(prompt, "只改变发型") || strings.Contains(prompt, "评分") {
		t.Fatalf("提示词必须限定改动范围: %q", prompt)
	}

	bare := previewPrompt(PreviewWork{StyleID: CustomDirectionID})
	if strings.Contains(bare, "「") {
		t.Fatalf("没有方向名时不应出现空引号: %q", bare)
	}
}
