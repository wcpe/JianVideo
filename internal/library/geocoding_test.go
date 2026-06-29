package library

import "testing"

// TestOfflineGeocoder_KnownCoordinates 覆盖若干已知城市坐标，验证就近匹配到「省·市」可读地名。
func TestOfflineGeocoder_KnownCoordinates(t *testing.T) {
	g := NewOfflineGeocoder()
	cases := []struct {
		name string
		lat  float64
		lon  float64
		want string
	}{
		// 城市中心点附近，应精确命中对应城市
		{name: "杭州西湖", lat: 30.2592, lon: 120.1300, want: "浙江·杭州"},
		{name: "北京天安门", lat: 39.9087, lon: 116.3975, want: "北京·北京"},
		{name: "上海外滩", lat: 31.2400, lon: 121.4900, want: "上海·上海"},
		{name: "广州塔", lat: 23.1090, lon: 113.3240, want: "广东·广州"},
		{name: "成都春熙路", lat: 30.6570, lon: 104.0810, want: "四川·成都"},
		// 城市中心略有偏移仍应就近匹配到同城
		{name: "杭州偏移", lat: 30.30, lon: 120.20, want: "浙江·杭州"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := g.Resolve(c.lat, c.lon)
			if !ok {
				t.Fatalf("坐标 (%f,%f) 期望命中地名 %q，实际未命中", c.lat, c.lon, c.want)
			}
			if got != c.want {
				t.Fatalf("坐标 (%f,%f) 期望地名 %q，实际 %q", c.lat, c.lon, c.want, got)
			}
		})
	}
}

// TestOfflineGeocoder_NoCoordinate 无坐标（0,0 或越界）时不解析地名。
func TestOfflineGeocoder_NoCoordinate(t *testing.T) {
	g := NewOfflineGeocoder()
	cases := []struct {
		name string
		lat  float64
		lon  float64
	}{
		{name: "零坐标", lat: 0, lon: 0},
		{name: "纬度越界", lat: 95, lon: 120},
		{name: "经度越界", lat: 30, lon: 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, ok := g.Resolve(c.lat, c.lon); ok {
				t.Fatalf("坐标 (%f,%f) 期望不命中，实际得到 %q", c.lat, c.lon, got)
			}
		})
	}
}

// TestOfflineGeocoder_OutOfRange 远离所有内置城市（如海洋中心）时超出就近半径，不臆造地名。
func TestOfflineGeocoder_OutOfRange(t *testing.T) {
	g := NewOfflineGeocoder()
	// 太平洋中部，远离任何内置城市
	if got, ok := g.Resolve(0.0, -160.0); ok {
		t.Fatalf("远海坐标期望不命中，实际得到 %q", got)
	}
}

// TestHaversineDistance 验证球面距离计算：同点为 0，已知城市间距量级合理。
func TestHaversineDistance(t *testing.T) {
	if d := haversineKm(30.2592, 120.1300, 30.2592, 120.1300); d != 0 {
		t.Fatalf("同点距离应为 0，实际 %f", d)
	}
	// 北京—上海直线约 1060~1070km，给一个宽松区间避免脆弱断言
	d := haversineKm(39.9087, 116.3975, 31.2400, 121.4900)
	if d < 1000 || d > 1150 {
		t.Fatalf("北京—上海距离应约 1060km，实际 %f", d)
	}
}
