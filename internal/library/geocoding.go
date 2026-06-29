package library

import (
	"fmt"
	"math"
)

// Geocoder 逆地理编码服务：把 GPS 经纬度解析为可读地名（粗粒度：省 / 市级）。
// 供时间轴分组头 / 聚合处展示（FR-147，见 ADR-0050）。
// 采用离线方案：不联网、不外泄坐标，契合本地媒体服务器定位。
type Geocoder interface {
	// Resolve 将经纬度解析为可读地名（如「浙江·杭州」）。
	// 命中返回地名与 true；无坐标、坐标越界或无就近匹配时返回空串与 false。
	Resolve(lat, lon float64) (string, bool)
}

// cityAnchor 内置城市锚点：以城市中心经纬度代表该市，就近匹配。
// 粗粒度数据集，仅覆盖中国主要省会 / 直辖市，满足聚合展示需求、数据体量可控。
type cityAnchor struct {
	province string  // 省 / 直辖市名
	city     string  // 市名
	lat      float64 // 城市中心纬度
	lon      float64 // 城市中心经度
}

// builtInCityAnchors 内置粗粒度城市锚点表。
// 直辖市的 province 与 city 同名（如「北京·北京」）。
// 经纬度取各市中心近似值，仅用于就近归类，不追求高精度。
var builtInCityAnchors = []cityAnchor{
	{province: "北京", city: "北京", lat: 39.9042, lon: 116.4074},
	{province: "上海", city: "上海", lat: 31.2304, lon: 121.4737},
	{province: "天津", city: "天津", lat: 39.0842, lon: 117.2009},
	{province: "重庆", city: "重庆", lat: 29.5630, lon: 106.5516},
	{province: "浙江", city: "杭州", lat: 30.2741, lon: 120.1551},
	{province: "江苏", city: "南京", lat: 32.0603, lon: 118.7969},
	{province: "江苏", city: "苏州", lat: 31.2989, lon: 120.5853},
	{province: "广东", city: "广州", lat: 23.1291, lon: 113.2644},
	{province: "广东", city: "深圳", lat: 22.5431, lon: 114.0579},
	{province: "四川", city: "成都", lat: 30.5728, lon: 104.0668},
	{province: "湖北", city: "武汉", lat: 30.5928, lon: 114.3055},
	{province: "陕西", city: "西安", lat: 34.3416, lon: 108.9398},
	{province: "湖南", city: "长沙", lat: 28.2282, lon: 112.9388},
	{province: "福建", city: "福州", lat: 26.0745, lon: 119.2965},
	{province: "福建", city: "厦门", lat: 24.4798, lon: 118.0894},
	{province: "山东", city: "济南", lat: 36.6512, lon: 117.1201},
	{province: "山东", city: "青岛", lat: 36.0671, lon: 120.3826},
	{province: "河南", city: "郑州", lat: 34.7466, lon: 113.6254},
	{province: "辽宁", city: "沈阳", lat: 41.8057, lon: 123.4315},
	{province: "辽宁", city: "大连", lat: 38.9140, lon: 121.6147},
	{province: "黑龙江", city: "哈尔滨", lat: 45.8038, lon: 126.5349},
	{province: "吉林", city: "长春", lat: 43.8171, lon: 125.3235},
	{province: "河北", city: "石家庄", lat: 38.0428, lon: 114.5149},
	{province: "山西", city: "太原", lat: 37.8706, lon: 112.5489},
	{province: "安徽", city: "合肥", lat: 31.8206, lon: 117.2272},
	{province: "江西", city: "南昌", lat: 28.6820, lon: 115.8579},
	{province: "云南", city: "昆明", lat: 25.0389, lon: 102.7183},
	{province: "贵州", city: "贵阳", lat: 26.6470, lon: 106.6302},
	{province: "广西", city: "南宁", lat: 22.8170, lon: 108.3665},
	{province: "甘肃", city: "兰州", lat: 36.0611, lon: 103.8343},
	{province: "内蒙古", city: "呼和浩特", lat: 40.8424, lon: 111.7490},
	{province: "新疆", city: "乌鲁木齐", lat: 43.8256, lon: 87.6168},
	{province: "宁夏", city: "银川", lat: 38.4872, lon: 106.2309},
	{province: "青海", city: "西宁", lat: 36.6171, lon: 101.7782},
	{province: "西藏", city: "拉萨", lat: 29.6520, lon: 91.1721},
	{province: "海南", city: "海口", lat: 20.0440, lon: 110.1999},
}

// maxMatchRadiusKm 就近匹配的最大半径（公里）。
// 超过此距离视为不在内置城市覆盖范围内，不臆造地名。
// 取值较宽松（覆盖城市辖区与近郊），但足以排除远海 / 境外坐标。
const maxMatchRadiusKm = 150.0

// earthRadiusKm 地球平均半径（公里），用于球面距离计算。
const earthRadiusKm = 6371.0

// offlineGeocoder 离线逆地理编码实现：在内置城市锚点表中就近匹配。
type offlineGeocoder struct {
	anchors []cityAnchor
}

// NewOfflineGeocoder 创建基于内置粗粒度城市表的离线逆地理编码器。
func NewOfflineGeocoder() Geocoder {
	return &offlineGeocoder{anchors: builtInCityAnchors}
}

// Resolve 就近匹配内置城市锚点，返回「省·市」可读地名。
// 坐标无效（0,0 或越界）或最近城市超出就近半径时返回 false。
func (g *offlineGeocoder) Resolve(lat, lon float64) (string, bool) {
	if !validCoordinate(lat, lon) {
		return "", false
	}

	bestDist := math.Inf(1)
	bestIdx := -1
	for i, a := range g.anchors {
		d := haversineKm(lat, lon, a.lat, a.lon)
		if d < bestDist {
			bestDist = d
			bestIdx = i
		}
	}

	if bestIdx < 0 || bestDist > maxMatchRadiusKm {
		return "", false
	}
	a := g.anchors[bestIdx]
	return fmt.Sprintf("%s·%s", a.province, a.city), true
}

// validCoordinate 校验经纬度是否有效：纬度 [-90,90]、经度 [-180,180]，且非 (0,0)。
// (0,0) 落在几内亚湾，视作「无 GPS」的常见占位值，不解析。
func validCoordinate(lat, lon float64) bool {
	if lat == 0 && lon == 0 {
		return false
	}
	if lat < -90 || lat > 90 {
		return false
	}
	if lon < -180 || lon > 180 {
		return false
	}
	return true
}

// haversineKm 计算两点间球面大圆距离（公里）。纯函数，无 IO。
func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	rLat1 := toRadians(lat1)
	rLat2 := toRadians(lat2)
	dLat := toRadians(lat2 - lat1)
	dLon := toRadians(lon2 - lon1)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rLat1)*math.Cos(rLat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKm * c
}

// toRadians 角度转弧度。
func toRadians(deg float64) float64 {
	return deg * math.Pi / 180
}
