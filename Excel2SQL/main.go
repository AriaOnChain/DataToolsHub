package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/xuri/excelize/v2"
)

// TableSchema 表结构信息
type TableSchema struct {
	TableName  string
	Columns    []ColumnInfo
	PrimaryKey string
	Indexes    []IndexInfo
}

// ColumnInfo 列信息
type ColumnInfo struct {
	Name         string
	DataType     string
	Length       int
	IsNullable   bool
	IsPrimaryKey bool
	DefaultValue string
	Comment      string
}

// IndexInfo 索引信息
type IndexInfo struct {
	Name     string
	Columns  []string
	IsUnique bool
}

// UploadResponse 上传响应
type UploadResponse struct {
	Success     bool         `json:"success"`
	Message     string       `json:"message"`
	DDL         []string     `json:"ddl,omitempty"`
	DML         []string     `json:"dml,omitempty"`
	TableSchema *TableSchema `json:"table_schema,omitempty"`
}

func main() {
	r := gin.Default()

	// 设置模板目录
	r.LoadHTMLGlob("templates/*")

	// 静态文件服务
	r.Static("/static", "./static")

	// 首页
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{
			"title": "Excel数据分析工具",
		})
	})

	// 下载模板
	r.GET("/download-template", downloadTemplate)

	// 上传并分析Excel
	r.POST("/upload", uploadAndAnalyze)

	// 生成SQL语句
	r.POST("/generate-sql", generateSQL)

	log.Println("服务器启动在 http://localhost:8080")
	r.Run(":8080")
}

// downloadTemplate 下载Excel模板
func downloadTemplate(c *gin.Context) {
	f := excelize.NewFile()
	defer f.Close()

	// 创建表结构定义工作表
	sheet1 := "表结构定义"
	f.SetSheetName("Sheet1", sheet1)

	// 设置表头
	headers := []string{"字段名", "数据类型", "长度", "是否可为空", "是否主键", "默认值", "字段说明"}
	for i, header := range headers {
		cell := fmt.Sprintf("%c1", 'A'+i)
		f.SetCellValue(sheet1, cell, header)
	}

	// 设置样式
	style, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 12},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#E0E0E0"}, Pattern: 1},
	})
	f.SetCellStyle(sheet1, "A1", "G1", style)

	// 设置列宽
	f.SetColWidth(sheet1, "A", "G", 15)

	// 添加示例数据
	examples := [][]interface{}{
		{"id", "INT", 11, "NO", "YES", "", "主键ID"},
		{"name", "VARCHAR", 50, "NO", "NO", "", "姓名"},
		{"age", "INT", 3, "YES", "NO", "0", "年龄"},
		{"email", "VARCHAR", 100, "YES", "NO", "", "邮箱"},
		{"created_at", "DATETIME", 0, "NO", "NO", "CURRENT_TIMESTAMP", "创建时间"},
	}

	for row, example := range examples {
		for col, value := range example {
			cell := fmt.Sprintf("%c%d", 'A'+col, row+2)
			f.SetCellValue(sheet1, cell, value)
		}
	}

	// 创建数据示例工作表
	sheet2 := "数据示例"
	f.NewSheet(sheet2)
	f.SetCellValue(sheet2, "A1", "请根据表结构填写数据，第一行为字段名")

	// 设置数据示例表头
	dataHeaders := []string{"id", "name", "age", "email", "created_at"}
	for i, header := range dataHeaders {
		cell := fmt.Sprintf("%c%d", 'A'+i, 2)
		f.SetCellValue(sheet2, cell, header)
	}

	// 添加示例数据
	dataExamples := [][]interface{}{
		{1, "张三", 25, "zhangsan@example.com", "2024-01-01 00:00:00"},
		{2, "李四", 30, "lisi@example.com", "2024-01-01 00:00:00"},
	}

	for row, dataExample := range dataExamples {
		for col, value := range dataExample {
			cell := fmt.Sprintf("%c%d", 'A'+col, row+3)
			f.SetCellValue(sheet2, cell, value)
		}
	}

	// 创建说明工作表
	sheet3 := "填写说明"
	f.NewSheet(sheet3)

	instructions := [][]string{
		{"填写说明：", ""},
		{"1. 表结构定义", "在'表结构定义'工作表中填写表的字段信息"},
		{"2. 数据类型", "支持的数据类型：INT, VARCHAR, TEXT, DATETIME, DATE, DECIMAL, FLOAT, DOUBLE, BOOLEAN"},
		{"3. 长度", "对于VARCHAR类型，指定最大长度；对于DECIMAL，格式为'总长度,小数位数'"},
		{"4. 是否可为空", "填写'YES'或'NO'"},
		{"5. 是否主键", "填写'YES'或'NO'，只能有一个主键"},
		{"6. 默认值", "填写字段的默认值，如：'0', 'CURRENT_TIMESTAMP'等"},
		{"7. 数据填写", "在'数据示例'工作表中填写实际数据，第一行为字段名"},
		{"", ""},
		{"注意事项：", ""},
		{"- 表名将从文件名自动读取", ""},
		{"- 建议先定义好表结构，再填写数据", ""},
		{"- 确保数据类型与Excel中的格式匹配", ""},
	}

	for row, instruction := range instructions {
		for col, text := range instruction {
			cell := fmt.Sprintf("%c%d", 'A'+col, row+1)
			f.SetCellValue(sheet3, cell, text)
		}
	}

	// 设置默认工作表
	f.SetActiveSheet(0)

	// 设置响应头
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", "attachment; filename=excel_template.xlsx")

	// 写入响应
	if err := f.Write(c.Writer); err != nil {
		log.Printf("写入模板失败: %v", err)
		c.String(http.StatusInternalServerError, "下载模板失败")
	}
}

// uploadAndAnalyze 上传并分析Excel
func uploadAndAnalyze(c *gin.Context) {
	file, err := c.FormFile("excel_file")
	if err != nil {
		c.JSON(http.StatusBadRequest, UploadResponse{
			Success: false,
			Message: "请选择要上传的文件",
		})
		return
	}

	// 检查文件扩展名
	if ext := filepath.Ext(file.Filename); ext != ".xlsx" && ext != ".xls" {
		c.JSON(http.StatusBadRequest, UploadResponse{
			Success: false,
			Message: "请上传Excel文件(.xlsx或.xls格式)",
		})
		return
	}

	// 保存临时文件
	tempPath := filepath.Join(os.TempDir(), fmt.Sprintf("upload_%d_%s", time.Now().Unix(), file.Filename))
	if err := c.SaveUploadedFile(file, tempPath); err != nil {
		log.Printf("保存文件失败: %v", err)
		c.JSON(http.StatusInternalServerError, UploadResponse{
			Success: false,
			Message: "保存文件失败",
		})
		return
	}
	defer os.Remove(tempPath)

	// 分析Excel
	schema, ddl, dml, err := analyzeExcel(tempPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, UploadResponse{
			Success: false,
			Message: fmt.Sprintf("分析Excel失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, UploadResponse{
		Success:     true,
		Message:     "分析成功",
		DDL:         ddl,
		DML:         dml,
		TableSchema: schema,
	})
}

// generateSQL 生成SQL语句
func generateSQL(c *gin.Context) {
	var req struct {
		TableName string          `json:"table_name"`
		Columns   []ColumnInfo    `json:"columns"`
		Data      [][]interface{} `json:"data"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}

	ddl := generateDDL(req.TableName, req.Columns)
	dml := generateDML(req.TableName, req.Columns, req.Data)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"ddl":     ddl,
		"dml":     dml,
	})
}

// analyzeExcel 分析Excel文件
func analyzeExcel(filePath string) (*TableSchema, []string, []string, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("打开Excel文件失败: %v", err)
	}
	defer f.Close()

	// 获取表名（从文件名）
	tableName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	tableName = sanitizeTableName(tableName)

	// 读取表结构定义工作表
	sheetName := "表结构定义"
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("读取表结构定义失败: %v", err)
	}

	if len(rows) < 2 {
		return nil, nil, nil, fmt.Errorf("表结构定义至少需要一行表头和一行动数据")
	}

	// 解析表结构
	var columns []ColumnInfo
	var primaryKey string

	for i, row := range rows[1:] { // 跳过表头
		if len(row) < 7 {
			continue
		}

		col := ColumnInfo{
			Name:         strings.TrimSpace(row[0]),
			DataType:     strings.ToUpper(strings.TrimSpace(row[1])),
			IsNullable:   strings.ToUpper(strings.TrimSpace(row[3])) == "YES",
			IsPrimaryKey: strings.ToUpper(strings.TrimSpace(row[4])) == "YES",
			DefaultValue: strings.TrimSpace(row[5]),
			Comment:      strings.TrimSpace(row[6]),
		}

		// 解析长度
		if len(row) > 2 && row[2] != "" {
			length, _ := strconv.Atoi(strings.TrimSpace(row[2]))
			col.Length = length
		}

		if col.Name == "" {
			continue
		}

		if col.IsPrimaryKey {
			if primaryKey != "" {
				return nil, nil, nil, fmt.Errorf("只能设置一个主键，第%d行也设置了主键", i+2)
			}
			primaryKey = col.Name
		}

		columns = append(columns, col)
	}

	if len(columns) == 0 {
		return nil, nil, nil, fmt.Errorf("未找到有效的表结构定义")
	}

	schema := &TableSchema{
		TableName:  tableName,
		Columns:    columns,
		PrimaryKey: primaryKey,
	}

	// 读取数据
	var data [][]interface{}
	dataSheet := "数据示例"
	if rows, err := f.GetRows(dataSheet); err == nil && len(rows) > 2 {
		// 修改这里：数据示例的第一行是提示文字，第二行是表头，第三行开始是数据
		if len(rows) > 2 {
			for i := 2; i < len(rows); i++ {
				row := rows[i]
				rowData := make([]interface{}, len(row))
				for j := range row {
					rowData[j] = row[j]
				}
				// 检查是否为空行
				hasData := false
				for _, v := range rowData {
					if v != nil && v != "" {
						hasData = true
						break
					}
				}
				if hasData {
					data = append(data, rowData)
				}
			}
		}
	}

	// 生成DDL和DML
	ddl := generateDDL(tableName, columns)
	dml := generateDML(tableName, columns, data)

	return schema, ddl, dml, nil
}

// generateDDL 生成DDL语句
func generateDDL(tableName string, columns []ColumnInfo) []string {
	var ddl []string

	// 创建表语句
	createTable := fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s` (", tableName)

	var columnDefs []string
	var primaryKeys []string

	for _, col := range columns {
		// 构建列定义
		colDef := fmt.Sprintf("  `%s` %s", col.Name, col.DataType)

		if col.Length > 0 && col.DataType != "DATETIME" && col.DataType != "DATE" {
			if col.DataType == "DECIMAL" {
				// DECIMAL需要特殊处理，这里简化处理
				colDef = fmt.Sprintf("  `%s` %s(%d,%d)", col.Name, col.DataType, col.Length/10, col.Length%10)
			} else if col.DataType != "TEXT" && col.DataType != "LONGTEXT" {
				colDef = fmt.Sprintf("  `%s` %s(%d)", col.Name, col.DataType, col.Length)
			}
		}

		if !col.IsNullable {
			colDef += " NOT NULL"
		}

		if col.DefaultValue != "" && col.DefaultValue != "NULL" {
			if col.DefaultValue == "CURRENT_TIMESTAMP" {
				colDef += " DEFAULT CURRENT_TIMESTAMP"
			} else {
				colDef += fmt.Sprintf(" DEFAULT '%s'", col.DefaultValue)
			}
		}

		if col.Comment != "" {
			colDef += fmt.Sprintf(" COMMENT '%s'", col.Comment)
		}

		columnDefs = append(columnDefs, colDef)

		if col.IsPrimaryKey {
			primaryKeys = append(primaryKeys, fmt.Sprintf("`%s`", col.Name))
		}
	}

	// 添加主键约束
	if len(primaryKeys) > 0 {
		columnDefs = append(columnDefs, fmt.Sprintf("  PRIMARY KEY (%s)", strings.Join(primaryKeys, ", ")))
	}

	createTable += "\n" + strings.Join(columnDefs, ",\n") + "\n) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='自动生成的表';"

	ddl = append(ddl, createTable)

	return ddl
}

// generateDML 生成DML语句
func generateDML(tableName string, columns []ColumnInfo, data [][]interface{}) []string {
	var dml []string

	if len(data) == 0 {
		return dml
	}

	// 获取列名列表
	var columnNames []string
	for _, col := range columns {
		columnNames = append(columnNames, fmt.Sprintf("`%s`", col.Name))
	}

	// 生成INSERT语句
	for _, row := range data {
		if len(row) == 0 {
			continue
		}

		var values []string
		for i, val := range row {
			if val == nil || val == "" {
				if i < len(columns) && !columns[i].IsNullable {
					values = append(values, "''")
				} else {
					values = append(values, "NULL")
				}
				continue
			}

			switch v := val.(type) {
			case string:
				// 转义单引号
				escaped := strings.ReplaceAll(v, "'", "''")
				values = append(values, fmt.Sprintf("'%s'", escaped))
			case float64:
				// 检查是否为整数
				if v == float64(int(v)) {
					values = append(values, fmt.Sprintf("%d", int(v)))
				} else {
					values = append(values, fmt.Sprintf("%v", v))
				}
			case int:
				values = append(values, fmt.Sprintf("%d", v))
			case int64:
				values = append(values, fmt.Sprintf("%d", v))
			default:
				values = append(values, fmt.Sprintf("'%v'", v))
			}
		}

		// 确保values和columnNames长度匹配
		minLen := len(columnNames)
		if len(values) < minLen {
			minLen = len(values)
		}

		insertSQL := fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s);",
			tableName,
			strings.Join(columnNames[:minLen], ", "),
			strings.Join(values[:minLen], ", "))

		dml = append(dml, insertSQL)
	}

	return dml
}

// sanitizeTableName 清理表名
func sanitizeTableName(name string) string {
	// 移除特殊字符，只保留字母、数字和下划线
	reg := regexp.MustCompile(`[^a-zA-Z0-9_]`)
	name = reg.ReplaceAllString(name, "_")

	// 转换为小写
	name = strings.ToLower(name)

	// 确保不是以数字开头
	if len(name) > 0 && name[0] >= '0' && name[0] <= '9' {
		name = "t_" + name
	}

	// 移除连续的下划线
	reg2 := regexp.MustCompile(`_+`)
	name = reg2.ReplaceAllString(name, "_")

	// 移除首尾下划线
	name = strings.Trim(name, "_")

	if name == "" {
		name = "temp_table"
	}

	return name
}
