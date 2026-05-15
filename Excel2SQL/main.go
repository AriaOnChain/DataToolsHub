package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// MappingColumns 期望的映射列
var ExpectedMappingColumns = []string{"字段名称", "数据类型", "字段描述"}

// 默认配置
const (
	DefaultDirectorySheet   = "表目录"
	DefaultPartitionComment = "数据分区日期"
	DefaultStorageFormat    = "ORC"
)

// TableMeta 表元数据
type TableMeta struct {
	TableName       string
	TableComment    string
	PartitionField  string
	Grain           string
	StorageStrategy string
}

// FieldMapping 字段映射信息
type FieldMapping struct {
	FieldCategory   string
	FieldName       string
	DataType        string
	FieldDesc       string
	SourceTable     string
	SourceFieldEn   string
	FieldLogic      string
	JoinLogic       string
	MappingLogic    string
	IsPrimaryKey    string
	FilterCondition string
}

// JoinInfo JOIN信息
type JoinInfo struct {
	SourceTable string
	Alias       string
	JoinLogic   string
}

// GenerateResult 生成结果
type GenerateResult struct {
	Success    bool     `json:"success"`
	Message    string   `json:"message"`
	DDL        []string `json:"ddl,omitempty"`
	DML        []string `json:"dml,omitempty"`
	TableNames []string `json:"table_names,omitempty"`
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
			"title": "Hive表结构生成工具",
		})
	})

	// 下载模板
	r.GET("/download-template", downloadMappingTemplate)

	// 上传并生成SQL
	r.POST("/upload", uploadAndGenerate)

	// 下载生成的SQL
	r.GET("/download/:filename", downloadSQL)

	log.Println("服务器启动在 http://localhost:8080")
	r.Run(":8080")
}

// downloadMappingTemplate 下载映射模板
func downloadMappingTemplate(c *gin.Context) {
	f := excelize.NewFile()
	defer f.Close()

	// 创建表目录工作表
	dirSheet := DefaultDirectorySheet
	f.SetSheetName("Sheet1", dirSheet)

	// 设置表目录表头
	dirHeaders := []string{"表中文描述", "表英文名称", "分区字段", "粒度", "数据存储策略"}
	for i, header := range dirHeaders {
		cell := fmt.Sprintf("%c1", 'A'+i)
		f.SetCellValue(dirSheet, cell, header)
	}

	// 设置样式
	style, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 12},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#E0E0E0"}, Pattern: 1},
	})
	f.SetCellStyle(dirSheet, "A1", fmt.Sprintf("%c1", 'A'+len(dirHeaders)-1), style)

	// 添加表目录示例数据
	dirExamples := [][]interface{}{
		{"用户维度表", "dim_user", "dt", "日", "列式存储"},
		{"产品维度表", "dim_product", "dt", "日", "列式存储"},
		{"订单事实表", "fact_order", "dt", "日", "列式存储"},
	}

	for row, example := range dirExamples {
		for col, value := range example {
			cell := fmt.Sprintf("%c%d", 'A'+col, row+2)
			f.SetCellValue(dirSheet, cell, value)
		}
	}

	// 创建字段映射工作表示例
	mappingSheet := "用户维度表"
	f.NewSheet(mappingSheet)

	// 设置字段映射表头
	mappingHeaders := []string{"字段名称", "数据类型", "字段描述", "是否主键", "来源业务系统", "来源表", "来源字段英文名",
		"来源字段中文名", "字段复杂逻辑", "关联逻辑", "映射逻辑", "过滤条件"}
	for i, header := range mappingHeaders {
		cell := fmt.Sprintf("%c1", 'A'+i)
		f.SetCellValue(mappingSheet, cell, header)
	}
	f.SetCellStyle(mappingSheet, "A1", fmt.Sprintf("%c1", 'A'+len(mappingHeaders)-1), style)

	// 添加字段映射示例数据
	mappingExamples := [][]interface{}{
		{"主键", "user_id", "bigint", "用户ID", "ods_user", "id", "", "", "", "是", ""},
		{"基本信息", "user_name", "string", "用户名", "ods_user", "name", "", "", "", "", ""},
		{"基本信息", "age", "int", "年龄", "ods_user", "age", "", "", "", "", "age > 0"},
		{"扩展信息", "user_level", "string", "用户等级", "ods_user_ext", "level", "", "ods_user.id = ods_user_ext.user_id", "", "", ""},
		{"派生字段", "age_group", "string", "年龄段", "", "", "case when age < 18 then '未成年' when age < 60 then '成年' else '老年' end", "", "", "", ""},
	}

	for row, example := range mappingExamples {
		for col, value := range example {
			cell := fmt.Sprintf("%c%d", 'A'+col, row+2)
			f.SetCellValue(mappingSheet, cell, value)
		}
	}

	// 创建填写说明工作表
	instructionSheet := "填写说明"
	f.NewSheet(instructionSheet)

	instructions := [][]string{
		{"📋 填写说明", ""},
		{"", ""},
		{"1. 表目录", "定义所有表的元数据信息"},
		{"   - 表中文描述", "工作表的名称，也是生成SQL时的表注释"},
		{"   - 表英文名称", "实际创建的表名"},
		{"   - 分区字段", "分区字段名，默认为dt"},
		{"   - 粒度", "日/月/年等"},
		{"   - 数据存储策略", "列式存储/行式存储（对应ORC/TEXTFILE）"},
		{"", ""},
		{"2. 字段映射", "每个工作表对应一张表的字段映射关系（工作表名需与表目录中的表中文描述一致）"},
		{"   - 字段名称", "目标表的字段名"},
		{"   - 数据类型", "Hive支持的数据类型（bigint, string, int, decimal等）"},
		{"   - 字段描述", "字段注释"},
		{"   - 来源表", "数据来源表名"},
		{"   - 来源字段英文名", "来源表的字段名"},
		{"   - 字段逻辑", "字段转换逻辑说明（仅用于注释）"},
		{"   - 关联逻辑", "多表关联的ON条件"},
		{"   - 映射逻辑", "复杂的字段映射表达式（优先级高于来源字段）"},
		{"   - 是否主键", "标记主表（填'是'）"},
		{"   - 过滤条件", "WHERE条件，会与其他条件用AND连接"},
		{"", ""},
		{"⚠️ 注意事项", ""},
		{"• 表目录中的表中文描述必须与字段映射的工作表名称完全一致"},
		{"• 至少有一个字段标记为是否主键='是'，用于确定主表"},
		{"• 关联逻辑格式示例: t1.id = t2.user_id（会进行表名替换）"},
		{"• 映射逻辑优先级高于来源表+来源字段"},
		{"• 支持 ${TX_DATE} 变量作为分区参数"},
	}

	for row, instruction := range instructions {
		for col, text := range instruction {
			cell := fmt.Sprintf("%c%d", 'A'+col, row+1)
			f.SetCellValue(instructionSheet, cell, text)
		}
	}

	// 设置列宽
	f.SetColWidth(dirSheet, "A", "E", 20)
	f.SetColWidth(mappingSheet, "A", "K", 18)
	f.SetColWidth(instructionSheet, "A", "B", 30)

	// 设置默认工作表
	f.SetActiveSheet(0)

	// 设置响应头
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", "attachment; filename=hive_mapping_template.xlsx")

	// 写入响应
	if err := f.Write(c.Writer); err != nil {
		log.Printf("写入模板失败: %v", err)
		c.String(http.StatusInternalServerError, "下载模板失败")
	}
}

// uploadAndGenerate 上传并生成SQL
func uploadAndGenerate(c *gin.Context) {
	file, err := c.FormFile("excel_file")
	if err != nil {
		c.JSON(http.StatusBadRequest, GenerateResult{
			Success: false,
			Message: "请选择要上传的文件",
		})
		return
	}

	// 检查文件扩展名
	if ext := filepath.Ext(file.Filename); ext != ".xlsx" && ext != ".xls" {
		c.JSON(http.StatusBadRequest, GenerateResult{
			Success: false,
			Message: "请上传Excel文件(.xlsx或.xls格式)",
		})
		return
	}

	// 保存临时文件
	tempPath := filepath.Join(os.TempDir(), fmt.Sprintf("mapping_%d_%s", time.Now().Unix(), file.Filename))
	if err := c.SaveUploadedFile(file, tempPath); err != nil {
		log.Printf("保存文件失败: %v", err)
		c.JSON(http.StatusInternalServerError, GenerateResult{
			Success: false,
			Message: "保存文件失败",
		})
		return
	}
	defer os.Remove(tempPath)

	// 生成SQL
	result, err := processMappingFile(tempPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, GenerateResult{
			Success: false,
			Message: fmt.Sprintf("生成SQL失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// downloadSQL 下载生成的SQL文件
func downloadSQL(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" {
		c.String(http.StatusBadRequest, "文件名不能为空")
		return
	}

	// 安全检查，防止路径遍历
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		c.String(http.StatusBadRequest, "非法文件名")
		return
	}

	filePath := filepath.Join("./generated", filename)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.String(http.StatusNotFound, "文件不存在")
		return
	}

	c.File(filePath)
}

// processMappingFile 处理映射文件
func processMappingFile(filePath string) (*GenerateResult, error) {
	// 加载表目录
	tableDir, err := LoadTableDirectory(filePath)
	if err != nil {
		return nil, fmt.Errorf("加载表目录失败: %v", err)
	}

	// 打开Excel文件获取所有工作表
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %v", err)
	}
	defer f.Close()

	sheetList := f.GetSheetList()

	var allDDL []string
	var allDML []string
	var tableNames []string

	// 创建生成目录
	outputDir := "./generated"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("创建输出目录失败: %v", err)
	}

	for _, sheetName := range sheetList {
		if sheetName == DefaultDirectorySheet || sheetName == "填写说明" {
			continue
		}

		log.Printf("处理工作表: %s", sheetName)

		// 加载映射数据
		mappings, err := LoadMappingSheet(filePath, sheetName)
		if err != nil {
			log.Printf("警告: 加载工作表 %s 失败: %v", sheetName, err)
			continue
		}

		if len(mappings) == 0 {
			continue
		}

		// 获取表元数据
		tableMeta, ok := tableDir[sheetName]
		if !ok || tableMeta.TableName == "" {
			continue
		}

		tableNames = append(tableNames, tableMeta.TableName)

		// 生成DDL
		ddl := GenerateDDL(tableMeta, mappings)
		allDDL = append(allDDL, ddl)

		// 生成DML
		dml, err := GenerateDML(tableMeta, mappings)
		if err != nil {
			log.Printf("警告: 生成DML失败 [%s]: %v", sheetName, err)
			continue
		}
		allDML = append(allDML, dml)
	}

	if len(allDDL) == 0 {
		return nil, fmt.Errorf("没有生成任何SQL语句，请检查Excel格式是否正确")
	}

	// 保存到文件
	timestamp := time.Now().Format("20060102_150405")
	ddlFile := filepath.Join(outputDir, fmt.Sprintf("ddl_%s.sql", timestamp))
	dmlFile := filepath.Join(outputDir, fmt.Sprintf("dml_%s.sql", timestamp))

	if err := WriteOutput(ddlFile, strings.Join(allDDL, "\n\n\n")); err != nil {
		return nil, fmt.Errorf("保存DDL文件失败: %v", err)
	}

	if err := WriteOutput(dmlFile, strings.Join(allDML, "\n\n\n")); err != nil {
		return nil, fmt.Errorf("保存DML文件失败: %v", err)
	}

	return &GenerateResult{
		Success:    true,
		Message:    fmt.Sprintf("成功生成 %d 张表的SQL语句", len(tableNames)),
		DDL:        []string{fmt.Sprintf("/download/%s", filepath.Base(ddlFile))},
		DML:        []string{fmt.Sprintf("/download/%s", filepath.Base(dmlFile))},
		TableNames: tableNames,
	}, nil
}

// LoadTableDirectory 加载表目录
func LoadTableDirectory(filePath string) (map[string]*TableMeta, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	rows, err := f.GetRows(DefaultDirectorySheet)
	if err != nil {
		return nil, err
	}

	if len(rows) < 2 {
		return nil, fmt.Errorf("目录表数据为空")
	}

	// 获取表头
	headers := rows[0]
	colIndex := make(map[string]int)
	for i, header := range headers {
		colIndex[NormalizeCell(header)] = i
	}

	tableDir := make(map[string]*TableMeta)

	for rowIdx := 1; rowIdx < len(rows); rowIdx++ {
		row := rows[rowIdx]
		sheetName := getCellValue(row, colIndex, "表中文描述")
		if sheetName == "" {
			continue
		}

		partitionField := getCellValue(row, colIndex, "分区字段")
		if partitionField == "" {
			partitionField = "dt"
		}

		tableDir[sheetName] = &TableMeta{
			TableName:       getCellValue(row, colIndex, "表英文名称"),
			TableComment:    sheetName,
			PartitionField:  partitionField,
			Grain:           getCellValue(row, colIndex, "粒度"),
			StorageStrategy: getCellValue(row, colIndex, "数据存储策略"),
		}
	}

	return tableDir, nil
}

// LoadMappingSheet 加载映射工作表
func LoadMappingSheet(filePath, sheetName string) ([]FieldMapping, error) {
	headerRow, err := DetectMappingHeaderRow(filePath, sheetName, 8)
	if err != nil {
		return nil, err
	}
	if headerRow == -1 {
		return nil, fmt.Errorf("未找到表头")
	}

	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, err
	}

	if len(rows) <= headerRow {
		return nil, fmt.Errorf("数据行为空")
	}

	// 获取列索引映射
	headerRowData := rows[headerRow]
	colIndex := make(map[string]int)
	for i, col := range headerRowData {
		colName := NormalizeCell(col)
		colIndex[colName] = i
	}

	// 检查必要列
	requiredCols := []string{"字段名称", "字段描述", "数据类型"}
	for _, col := range requiredCols {
		if _, ok := colIndex[col]; !ok {
			return nil, fmt.Errorf("缺少必要列: %s", col)
		}
	}

	var mappings []FieldMapping

	for rowIdx := headerRow + 1; rowIdx < len(rows); rowIdx++ {
		row := rows[rowIdx]
		mapping := FieldMapping{}

		mapping.FieldName = getCellValue(row, colIndex, "字段名称")
		mapping.FieldDesc = getCellValue(row, colIndex, "字段描述")
		mapping.DataType = getCellValue(row, colIndex, "数据类型")
		mapping.SourceTable = getCellValue(row, colIndex, "来源表")
		mapping.SourceFieldEn = getCellValue(row, colIndex, "来源字段英文名")
		mapping.FieldLogic = getCellValue(row, colIndex, "字段逻辑")
		mapping.JoinLogic = getCellValue(row, colIndex, "关联逻辑")
		mapping.MappingLogic = getCellValue(row, colIndex, "映射逻辑")
		mapping.IsPrimaryKey = getCellValue(row, colIndex, "是否主键")
		mapping.FilterCondition = getCellValue(row, colIndex, "过滤条件")

		if mapping.FieldName == "" && mapping.FieldDesc == "" {
			continue
		}

		mappings = append(mappings, mapping)
	}

	return mappings, nil
}

// DetectMappingHeaderRow 检测映射表头行
func DetectMappingHeaderRow(filePath, sheetName string, scanRows int) (int, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return -1, err
	}
	defer f.Close()

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return -1, err
	}

	if len(rows) == 0 {
		return -1, nil
	}

	bestRow := -1
	bestScore := 0

	for rowIdx := 0; rowIdx < len(rows) && rowIdx < scanRows; rowIdx++ {
		score := 0
		for _, colValue := range rows[rowIdx] {
			for _, expected := range ExpectedMappingColumns {
				if NormalizeCell(colValue) == expected {
					score++
					break
				}
			}
		}
		if score > bestScore {
			bestScore = score
			bestRow = rowIdx
		}
	}

	if bestScore >= 3 {
		return bestRow, nil
	}
	return -1, nil
}

func getCellValue(row []string, colIndex map[string]int, colName string) string {
	if idx, ok := colIndex[colName]; ok && idx < len(row) {
		return NormalizeCell(row[idx])
	}
	return ""
}

// NormalizeCell 规范化单元格值
func NormalizeCell(value interface{}) string {
	if value == nil {
		return ""
	}

	var str string
	switch v := value.(type) {
	case string:
		str = v
	case float64:
		if v == float64(int(v)) {
			str = fmt.Sprintf("%d", int(v))
		} else {
			str = fmt.Sprintf("%g", v)
		}
	case int:
		str = fmt.Sprintf("%d", v)
	case int64:
		str = fmt.Sprintf("%d", v)
	default:
		str = fmt.Sprintf("%v", v)
	}

	str = strings.ReplaceAll(str, "\u00A0", " ")
	str = strings.TrimSpace(str)

	if strings.ToLower(str) == "nan" {
		return ""
	}
	return str
}

// IsPlaceholder 判断是否为占位符
func IsPlaceholder(value string) bool {
	value = NormalizeCell(value)
	return value == "" || value == "-" || value == "—"
}

// EscapeSQLComment 转义SQL注释
func EscapeSQLComment(text string) string {
	return strings.ReplaceAll(NormalizeCell(text), "'", "\\'")
}

// ResolveStorageFormat 解析存储格式
func ResolveStorageFormat(storageStrategy string) string {
	strategy := strings.ToLower(NormalizeCell(storageStrategy))
	storageMap := map[string]string{
		"列式存储":     "ORC",
		"parquet":  "PARQUET",
		"orc":      "ORC",
		"textfile": "TEXTFILE",
		"文本存储":     "TEXTFILE",
		"行式存储":     "TEXTFILE",
	}

	if format, ok := storageMap[strategy]; ok {
		return format
	}
	return DefaultStorageFormat
}

// ChooseBaseTable 选择主表
func ChooseBaseTable(mappings []FieldMapping) string {
	// 优先选择标记为主键的表
	for _, m := range mappings {
		if NormalizeCell(m.IsPrimaryKey) == "是" {
			sourceTable := NormalizeCell(m.SourceTable)
			if sourceTable != "" && !IsPlaceholder(sourceTable) {
				return sourceTable
			}
		}
	}

	// 否则选择第一个非占位符的来源表
	for _, m := range mappings {
		sourceTable := NormalizeCell(m.SourceTable)
		if !IsPlaceholder(sourceTable) {
			return sourceTable
		}
	}

	return ""
}

// ReplaceTableReferences 替换表引用
func ReplaceTableReferences(expression string, aliasReplacements map[string]string) string {
	result := expression
	for tableName, alias := range aliasReplacements {
		pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(tableName) + `\.`)
		result = pattern.ReplaceAllString(result, alias+".")
	}
	return result
}

// BuildColumnDefinition 构建列定义
func BuildColumnDefinition(mapping FieldMapping) string {
	fieldName := NormalizeCell(mapping.FieldName)
	dataType := NormalizeCell(mapping.DataType)
	if dataType == "" {
		dataType = "string"
	}
	fieldDesc := EscapeSQLComment(mapping.FieldDesc)
	return fmt.Sprintf("    %s %s COMMENT '%s'", fieldName, dataType, fieldDesc)
}

// BuildJoinAliasMappings 构建JOIN别名映射
func BuildJoinAliasMappings(mappings []FieldMapping, baseTable string) (map[string]string, map[string]string, []JoinInfo) {
	joinAliasMap := make(map[string]string)
	tableAliasReplacements := make(map[string]string)
	var joinInfos []JoinInfo

	tableAliasReplacements[baseTable] = "t1"
	aliasIndex := 2

	for _, m := range mappings {
		sourceTable := NormalizeCell(m.SourceTable)
		joinLogic := NormalizeCell(m.JoinLogic)

		if sourceTable == "" || sourceTable == baseTable || IsPlaceholder(sourceTable) {
			continue
		}

		joinKey := sourceTable + "|" + joinLogic
		if _, exists := joinAliasMap[joinKey]; exists {
			continue
		}

		alias := fmt.Sprintf("t%d", aliasIndex)
		joinAliasMap[joinKey] = alias
		if _, exists := tableAliasReplacements[sourceTable]; !exists {
			tableAliasReplacements[sourceTable] = alias
		}

		joinInfos = append(joinInfos, JoinInfo{
			SourceTable: sourceTable,
			Alias:       alias,
			JoinLogic:   joinLogic,
		})

		aliasIndex++
	}

	return joinAliasMap, tableAliasReplacements, joinInfos
}

// BuildSelectExpression 构建SELECT表达式
func BuildSelectExpression(mapping FieldMapping, baseTable string, joinAliasMap map[string]string, tableAliasReplacements map[string]string) string {
	fieldName := NormalizeCell(mapping.FieldName)
	sourceTable := NormalizeCell(mapping.SourceTable)
	sourceField := NormalizeCell(mapping.SourceFieldEn)
	mappingLogic := NormalizeCell(mapping.MappingLogic)
	joinLogic := NormalizeCell(mapping.JoinLogic)

	var sourceAlias string
	if sourceTable == baseTable {
		sourceAlias = "t1"
	} else {
		joinKey := sourceTable + "|" + joinLogic
		if alias, ok := joinAliasMap[joinKey]; ok {
			sourceAlias = alias
		} else if alias, ok := tableAliasReplacements[sourceTable]; ok {
			sourceAlias = alias
		} else {
			sourceAlias = sourceTable
		}
	}

	var expression string
	if mappingLogic != "" && !IsPlaceholder(mappingLogic) {
		expression = ReplaceTableReferences(mappingLogic, tableAliasReplacements) + " AS " + fieldName
	} else if IsPlaceholder(sourceTable) || IsPlaceholder(sourceField) {
		expression = "NULL AS " + fieldName
	} else {
		expression = fmt.Sprintf("%s.%s AS %s", sourceAlias, sourceField, fieldName)
	}

	return expression
}

// CollectFilterConditions 收集过滤条件
func CollectFilterConditions(mappings []FieldMapping, tableAliasReplacements map[string]string, partitionField string) []string {
	conditions := []string{fmt.Sprintf("t1.%s = '${TX_DATE}'", partitionField)}
	seen := make(map[string]bool)

	for _, m := range mappings {
		filterCondition := NormalizeCell(m.FilterCondition)
		if IsPlaceholder(filterCondition) {
			continue
		}
		filterCondition = ReplaceTableReferences(filterCondition, tableAliasReplacements)
		if !seen[filterCondition] {
			conditions = append(conditions, filterCondition)
			seen[filterCondition] = true
		}
	}

	return conditions
}

// GenerateDDL 生成DDL语句
func GenerateDDL(tableMeta *TableMeta, mappings []FieldMapping) string {
	tableName := tableMeta.TableName
	partitionField := tableMeta.PartitionField
	tableComment := tableMeta.TableComment
	storageFormat := ResolveStorageFormat(tableMeta.StorageStrategy)

	var columnLines []string
	for _, m := range mappings {
		columnLines = append(columnLines, BuildColumnDefinition(m))
	}

	ddlLines := []string{
		fmt.Sprintf("-- %s", tableComment),
		fmt.Sprintf("DROP TABLE IF EXISTS %s;", tableName),
		fmt.Sprintf("CREATE TABLE %s (", tableName),
		strings.Join(columnLines, ",\n"),
		")",
		fmt.Sprintf("COMMENT '%s'", EscapeSQLComment(tableComment)),
		fmt.Sprintf("PARTITIONED BY (%s string COMMENT '%s')", partitionField, DefaultPartitionComment),
	}

	if storageFormat == "TEXTFILE" {
		ddlLines = append(ddlLines,
			"ROW FORMAT DELIMITED",
			"FIELDS TERMINATED BY '\\t'",
			"LINES TERMINATED BY '\\n'",
			"STORED AS TEXTFILE;",
		)
	} else {
		ddlLines = append(ddlLines, fmt.Sprintf("STORED AS %s;", storageFormat))
	}

	ddlLines = append(ddlLines,
		fmt.Sprintf("ALTER TABLE %s ADD IF NOT EXISTS PARTITION (%s = '${TX_DATE}');", tableName, partitionField),
	)

	return strings.Join(ddlLines, "\n")
}

// GenerateDML 生成DML语句
func GenerateDML(tableMeta *TableMeta, mappings []FieldMapping) (string, error) {
	tableName := tableMeta.TableName
	partitionField := tableMeta.PartitionField
	tableComment := tableMeta.TableComment

	baseTable := ChooseBaseTable(mappings)
	if baseTable == "" {
		return "", fmt.Errorf("未找到可作为FROM的来源表")
	}

	joinAliasMap, tableAliasReplacements, joinInfos := BuildJoinAliasMappings(mappings, baseTable)

	var selectLines []string
	for _, m := range mappings {
		selectLines = append(selectLines, "    "+BuildSelectExpression(m, baseTable, joinAliasMap, tableAliasReplacements))
	}

	filterConditions := CollectFilterConditions(mappings, tableAliasReplacements, partitionField)

	dmlLines := []string{
		fmt.Sprintf("-- %s", tableComment),
		"SET hive.exec.dynamic.partition=true;",
		"SET hive.exec.dynamic.partition.mode=nonstrict;",
		"",
		fmt.Sprintf("INSERT OVERWRITE TABLE %s PARTITION (%s)", tableName, partitionField),
		"SELECT",
		strings.Join(selectLines, ",\n"),
		fmt.Sprintf("FROM %s t1", baseTable),
	}

	for _, joinInfo := range joinInfos {
		joinCondition := joinInfo.JoinLogic
		if joinCondition == "" || joinCondition == "-" {
			joinCondition = "1=1"
		}
		dmlLines = append(dmlLines, fmt.Sprintf("LEFT JOIN %s %s ON %s", joinInfo.SourceTable, joinInfo.Alias, joinCondition))
	}

	if len(filterConditions) > 0 {
		whereClause := strings.Join(filterConditions, " AND ")
		dmlLines = append(dmlLines, fmt.Sprintf("WHERE %s", whereClause))
	}

	dmlLines = append(dmlLines, ";")

	return strings.Join(dmlLines, "\n"), nil
}

// WriteOutput 写入输出文件
func WriteOutput(filePath, content string) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	content = strings.TrimSpace(content) + "\n"
	return os.WriteFile(filePath, []byte(content), 0644)
}
