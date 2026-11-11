package geoip

// countryZH 覆盖常见来源国家/地区的中文名。库里自带 zh-CN 名时也优先用这里，
// 统一口径（例如库里的“香港”在这里一律叫“中国香港”）。
var countryZH = map[string]string{
	"CN": "中国", "HK": "中国香港", "MO": "中国澳门", "TW": "中国台湾",
	"US": "美国", "CA": "加拿大", "MX": "墨西哥", "BR": "巴西", "AR": "阿根廷", "CL": "智利", "CO": "哥伦比亚", "PE": "秘鲁", "VE": "委内瑞拉",
	"GB": "英国", "IE": "爱尔兰", "FR": "法国", "DE": "德国", "NL": "荷兰", "BE": "比利时", "LU": "卢森堡", "CH": "瑞士", "AT": "奥地利",
	"IT": "意大利", "ES": "西班牙", "PT": "葡萄牙", "GR": "希腊", "SE": "瑞典", "NO": "挪威", "FI": "芬兰", "DK": "丹麦", "IS": "冰岛",
	"PL": "波兰", "CZ": "捷克", "SK": "斯洛伐克", "HU": "匈牙利", "RO": "罗马尼亚", "BG": "保加利亚", "RS": "塞尔维亚", "HR": "克罗地亚",
	"SI": "斯洛文尼亚", "UA": "乌克兰", "BY": "白俄罗斯", "MD": "摩尔多瓦", "LT": "立陶宛", "LV": "拉脱维亚", "EE": "爱沙尼亚", "RU": "俄罗斯",
	"TR": "土耳其", "IL": "以色列", "IR": "伊朗", "IQ": "伊拉克", "SA": "沙特阿拉伯", "AE": "阿联酋", "QA": "卡塔尔", "KW": "科威特",
	"JO": "约旦", "LB": "黎巴嫩", "SY": "叙利亚", "CY": "塞浦路斯", "GE": "格鲁吉亚", "AM": "亚美尼亚", "AZ": "阿塞拜疆", "KZ": "哈萨克斯坦",
	"UZ": "乌兹别克斯坦", "PK": "巴基斯坦", "IN": "印度", "BD": "孟加拉国", "LK": "斯里兰卡", "NP": "尼泊尔",
	"JP": "日本", "KR": "韩国", "KP": "朝鲜", "MN": "蒙古", "SG": "新加坡", "MY": "马来西亚", "TH": "泰国", "VN": "越南", "PH": "菲律宾",
	"ID": "印度尼西亚", "KH": "柬埔寨", "LA": "老挝", "MM": "缅甸", "AU": "澳大利亚", "NZ": "新西兰",
	"EG": "埃及", "ZA": "南非", "NG": "尼日利亚", "KE": "肯尼亚", "MA": "摩洛哥", "DZ": "阿尔及利亚", "TN": "突尼斯", "GH": "加纳",
	"SC": "塞舌尔", "PA": "巴拿马", "BZ": "伯利兹", "VG": "英属维尔京群岛", "KY": "开曼群岛",
}

// CountryName 返回国家/地区的中文名。
func CountryName(cc string, names map[string]string) string {
	if n, ok := countryZH[cc]; ok {
		return n
	}
	if n := names["zh-CN"]; n != "" {
		return n
	}
	if n := names["en"]; n != "" {
		return n
	}
	return cc
}

// provinceZH：dbip-city-lite 的省级名称只有英文。
var provinceZH = map[string]string{
	"Beijing": "北京", "Tianjin": "天津", "Shanghai": "上海", "Chongqing": "重庆",
	"Hebei": "河北", "Shanxi": "山西", "Liaoning": "辽宁", "Jilin": "吉林", "Heilongjiang": "黑龙江",
	"Jiangsu": "江苏", "Zhejiang": "浙江", "Anhui": "安徽", "Fujian": "福建", "Jiangxi": "江西",
	"Shandong": "山东", "Henan": "河南", "Hubei": "湖北", "Hunan": "湖南", "Guangdong": "广东",
	"Hainan": "海南", "Sichuan": "四川", "Guizhou": "贵州", "Yunnan": "云南", "Shaanxi": "陕西",
	"Gansu": "甘肃", "Qinghai": "青海", "Inner Mongolia": "内蒙古", "Guangxi": "广西",
	"Tibet": "西藏", "Ningxia": "宁夏", "Xinjiang": "新疆",
	"Guangxi Zhuang Autonomous Region": "广西", "Ningxia Hui Autonomous Region": "宁夏",
	"Xinjiang Uyghur Autonomous Region": "新疆", "Tibet Autonomous Region": "西藏",
	"Inner Mongolia Autonomous Region": "内蒙古",
}

// cityZH：常见城市（国内地级市为主，外加常见机房城市）。
// 国内没收录的城市（库里常是区县名，如 Shizishan）在展示时退回到省名。
var cityZH = map[string]string{
	// 直辖市
	"Beijing": "北京", "Shanghai": "上海", "Tianjin": "天津", "Chongqing": "重庆",
	// 省会与主要城市
	"Wuhan": "武汉", "Guangzhou": "广州", "Shenzhen": "深圳", "Hangzhou": "杭州", "Nanjing": "南京",
	"Chengdu": "成都", "Xi'an": "西安", "Xian": "西安", "Changsha": "长沙", "Zhengzhou": "郑州",
	"Jinan": "济南", "Qingdao": "青岛", "Shenyang": "沈阳", "Dalian": "大连", "Harbin": "哈尔滨",
	"Changchun": "长春", "Shijiazhuang": "石家庄", "Taiyuan": "太原", "Hefei": "合肥", "Fuzhou": "福州",
	"Xiamen": "厦门", "Nanchang": "南昌", "Kunming": "昆明", "Guiyang": "贵阳", "Nanning": "南宁",
	"Haikou": "海口", "Sanya": "三亚", "Lanzhou": "兰州", "Xining": "西宁", "Yinchuan": "银川",
	"Urumqi": "乌鲁木齐", "Lhasa": "拉萨", "Hohhot": "呼和浩特", "Baotou": "包头",
	"Suzhou": "苏州", "Wuxi": "无锡", "Changzhou": "常州", "Nantong": "南通", "Xuzhou": "徐州", "Yangzhou": "扬州",
	"Ningbo": "宁波", "Wenzhou": "温州", "Shaoxing": "绍兴", "Jiaxing": "嘉兴", "Jinhua": "金华", "Taizhou": "台州", "Huzhou": "湖州",
	"Foshan": "佛山", "Dongguan": "东莞", "Zhuhai": "珠海", "Zhongshan": "中山", "Huizhou": "惠州", "Shantou": "汕头", "Jiangmen": "江门", "Zhanjiang": "湛江",
	"Yantai": "烟台", "Weifang": "潍坊", "Linyi": "临沂", "Zibo": "淄博", "Jining": "济宁", "Weihai": "威海",
	"Luoyang": "洛阳", "Kaifeng": "开封", "Nanyang": "南阳", "Xinxiang": "新乡",
	"Yichang": "宜昌", "Xiangyang": "襄阳", "Jingzhou": "荆州", "Huangshi": "黄石", "Shiyan": "十堰", "Xiaogan": "孝感", "Huanggang": "黄冈", "Ezhou": "鄂州",
	"Zhuzhou": "株洲", "Xiangtan": "湘潭", "Hengyang": "衡阳", "Yueyang": "岳阳",
	"Mianyang": "绵阳", "Yibin": "宜宾", "Nanchong": "南充", "Leshan": "乐山",
	"Quanzhou": "泉州", "Zhangzhou": "漳州", "Putian": "莆田",
	"Wuhu": "芜湖", "Bengbu": "蚌埠", "Ganzhou": "赣州", "Jiujiang": "九江",
	"Tangshan": "唐山", "Baoding": "保定", "Langfang": "廊坊", "Handan": "邯郸", "Qinhuangdao": "秦皇岛",
	"Anshan": "鞍山", "Jilin City": "吉林", "Daqing": "大庆", "Liuzhou": "柳州", "Guilin": "桂林",
	"Baoji": "宝鸡", "Xianyang": "咸阳", "Datong": "大同", "Zunyi": "遵义", "Dali": "大理",
	"Hong Kong": "香港", "Macau": "澳门", "Taipei": "台北", "Kaohsiung": "高雄", "Taichung": "台中",
	// 海外常见机房/大城市
	"Singapore": "新加坡", "Tokyo": "东京", "Osaka": "大阪", "Seoul": "首尔", "Bangkok": "曼谷", "Hanoi": "河内",
	"Ho Chi Minh City": "胡志明市", "Kuala Lumpur": "吉隆坡", "Jakarta": "雅加达", "Manila": "马尼拉",
	"Mumbai": "孟买", "Delhi": "德里", "New Delhi": "新德里", "Bengaluru": "班加罗尔", "Bangalore": "班加罗尔", "Dubai": "迪拜", "Abu Dhabi": "阿布扎比",
	"Amsterdam": "阿姆斯特丹", "Frankfurt am Main": "法兰克福", "Frankfurt": "法兰克福", "Berlin": "柏林", "Munich": "慕尼黑", "Nuremberg": "纽伦堡", "Falkenstein": "法尔肯施泰因",
	"London": "伦敦", "Paris": "巴黎", "Roubaix": "鲁贝", "Gravelines": "格拉沃利讷", "Madrid": "马德里", "Milan": "米兰", "Rome": "罗马",
	"Stockholm": "斯德哥尔摩", "Helsinki": "赫尔辛基", "Oslo": "奥斯陆", "Copenhagen": "哥本哈根", "Warsaw": "华沙", "Prague": "布拉格",
	"Vienna": "维也纳", "Zurich": "苏黎世", "Brussels": "布鲁塞尔", "Dublin": "都柏林", "Bucharest": "布加勒斯特", "Sofia": "索非亚", "Kyiv": "基辅", "Kiev": "基辅",
	"Moscow": "莫斯科", "Saint Petersburg": "圣彼得堡", "Istanbul": "伊斯坦布尔", "Tehran": "德黑兰", "Cairo": "开罗",
	"New York": "纽约", "Los Angeles": "洛杉矶", "San Francisco": "旧金山", "San Jose": "圣何塞", "Santa Clara": "圣克拉拉", "Seattle": "西雅图",
	"Chicago": "芝加哥", "Dallas": "达拉斯", "Houston": "休斯顿", "Miami": "迈阿密", "Atlanta": "亚特兰大", "Ashburn": "阿什本",
	"Boston": "波士顿", "Washington": "华盛顿", "Phoenix": "菲尼克斯", "Denver": "丹佛", "Las Vegas": "拉斯维加斯", "Portland": "波特兰",
	"Mountain View": "山景城", "Cupertino": "库比蒂诺", "Clifton": "克利夫顿", "North Charleston": "北查尔斯顿", "Council Bluffs": "康瑟尔布拉夫斯", "The Dalles": "达尔斯",
	"Toronto": "多伦多", "Montreal": "蒙特利尔", "Vancouver": "温哥华", "Sydney": "悉尼", "Melbourne": "墨尔本",
	"Sao Paulo": "圣保罗", "São Paulo": "圣保罗", "Mexico City": "墨西哥城", "Johannesburg": "约翰内斯堡",
}

// ForeignFilter 是国家筛选里“中国以外”的取值：排除中国大陆和内网，港澳台算在内。
const ForeignFilter = "!中国"

// MatchCountry 判断展示用的国家名 label 是否满足筛选值 want（空表示不过滤）。
func MatchCountry(label, want string) bool {
	switch want {
	case "":
		return true
	case ForeignFilter:
		return label != "中国" && label != "内网"
	}
	return label == want
}
