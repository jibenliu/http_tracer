package job

import (
	"fmt"
	"getConfig/request"
	"github.com/spf13/viper"
	"github.com/xuri/excelize/v2"
	"strconv"
	"sync"
	"time"
)

type counter struct {
	rw    sync.Mutex //文件读写阻塞
	index int        //循环的次数
}

var cnt = counter{
	rw:    sync.Mutex{},
	index: 0,
}

// Start zysoft.com1QAZ2WSX
func Start() {
	configs := readConfig()
	count := 100
	f := excelize.NewFile()
	f.Path = "data.xlsx"
	index := f.NewSheet("Sheet1")
	sheetName := f.GetSheetName(f.GetActiveSheetIndex())
	f.SetActiveSheet(index)
	_ = f.SetCellValue(sheetName, "A1", "服务器名称")
	_ = f.SetCellValue(sheetName, "B1", "应用名称")
	_ = f.SetCellValue(sheetName, "C1", "http状态码")
	_ = f.SetCellValue(sheetName, "D1", "DNS查找时间")
	_ = f.SetCellValue(sheetName, "E1", "TCP连接时间")
	_ = f.SetCellValue(sheetName, "F1", "TLS握手时间")
	_ = f.SetCellValue(sheetName, "G1", "服务器处理时间")
	_ = f.SetCellValue(sheetName, "H1", "数据传输时间")
	_ = f.SetCellValue(sheetName, "I1", "总耗时")
	ticker := time.NewTicker(time.Minute * 1)
	for {
		select {
		case t := <-ticker.C:
			fmt.Println("ticker triggered at" + fmt.Sprintf(t.Format("2006-01-02 15:04:05 +08:00")))
			if cnt.index >= count {
				err := f.SaveAs("data.xlsx")
				if err != nil {
					panic("保存excel出错！")
				}
				return
			}
			sendReqAndSaveFile(configs, f)
		}
	}
}

func readConfig() map[string]interface{} {
	viper.SetConfigName("request")
	viper.SetConfigType("json")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("fatal error config file: %w", err))
	}
	return viper.AllSettings()
}

func sendReqAndSaveFile(configs map[string]interface{}, f *excelize.File) {
	cnt.rw.Lock()
	defer cnt.rw.Unlock()
	cnt.index++
	fmt.Println("遍历开始！")
	for serverName := range configs {
		apps := viper.GetStringMap(serverName)
		sheetName := f.GetSheetName(f.GetActiveSheetIndex())
		rows, _ := f.GetRows(sheetName)
		latestRowIndex := len(rows) + 1
		for name, config := range apps {
			conf := config.(map[string]interface{})
			if url, ok := conf["url"]; ok {
				resp, _ := request.TraceRequests().PostJson(url.(string), conf, request.Header{"X-ENGINEER-TOKEN": "XXXXX"})
				traceStart := resp.GetRequest().Client.GetTraceStat()
				_ = f.SetCellValue(sheetName, "A"+strconv.Itoa(latestRowIndex), serverName)
				_ = f.SetCellValue(sheetName, "B"+strconv.Itoa(latestRowIndex), name)
				_ = f.SetCellValue(sheetName, "C"+strconv.Itoa(latestRowIndex), traceStart.Status)
				_ = f.SetCellValue(sheetName, "D"+strconv.Itoa(latestRowIndex), traceStart.DNSLookup)
				_ = f.SetCellValue(sheetName, "E"+strconv.Itoa(latestRowIndex), traceStart.TCPConnection)
				_ = f.SetCellValue(sheetName, "F"+strconv.Itoa(latestRowIndex), traceStart.TLSHandshake)
				_ = f.SetCellValue(sheetName, "G"+strconv.Itoa(latestRowIndex), traceStart.ServerProcessing)
				_ = f.SetCellValue(sheetName, "H"+strconv.Itoa(latestRowIndex), traceStart.ContentTransfer)
				_ = f.SetCellValue(sheetName, "I"+strconv.Itoa(latestRowIndex), traceStart.Total)
				latestRowIndex++
				continue
			}
			fmt.Println("服务器", serverName, "应用", name, "配置错误，错误原因缺少url地址")
		}
	}
	fmt.Println("遍历完成！")
}
