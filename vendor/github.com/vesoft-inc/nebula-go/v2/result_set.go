/* Copyright (c) 2020 vesoft inc. All rights reserved.
*
* This source code is licensed under Apache 2.0 License,
* attached with Common Clause Condition 1.0, found in the LICENSES directory.
 */

package nebula_go

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/vesoft-inc/nebula-go/v2/nebula"
	"github.com/vesoft-inc/nebula-go/v2/nebula/graph"
)

type ResultSet struct {
	resp            *graph.ExecutionResponse
	columnNames     []string
	colNameIndexMap map[string]int
}

type Record struct {
	columnNames     *[]string
	_record         []*ValueWrapper
	colNameIndexMap *map[string]int
}

type Node struct {
	vertex          *nebula.Vertex
	tags            []string // tag name
	tagNameIndexMap map[string]int
}

type Relationship struct {
	edge *nebula.Edge
}

type segment struct {
	startNode    *Node
	relationship *Relationship
	endNode      *Node
}

type PathWrapper struct {
	path             *nebula.Path
	nodeList         []*Node
	relationshipList []*Relationship
	segments         []segment
}

type ErrorCode int64

const (
	ErrorCode_SUCCEEDED               ErrorCode = ErrorCode(graph.ErrorCode_SUCCEEDED)
	ErrorCode_E_DISCONNECTED          ErrorCode = ErrorCode(graph.ErrorCode_E_DISCONNECTED)
	ErrorCode_E_FAIL_TO_CONNECT       ErrorCode = ErrorCode(graph.ErrorCode_E_FAIL_TO_CONNECT)
	ErrorCode_E_RPC_FAILURE           ErrorCode = ErrorCode(graph.ErrorCode_E_RPC_FAILURE)
	ErrorCode_E_BAD_USERNAME_PASSWORD ErrorCode = ErrorCode(graph.ErrorCode_E_BAD_USERNAME_PASSWORD)
	ErrorCode_E_SESSION_INVALID       ErrorCode = ErrorCode(graph.ErrorCode_E_SESSION_INVALID)
	ErrorCode_E_SESSION_TIMEOUT       ErrorCode = ErrorCode(graph.ErrorCode_E_SESSION_TIMEOUT)
	ErrorCode_E_SYNTAX_ERROR          ErrorCode = ErrorCode(graph.ErrorCode_E_SYNTAX_ERROR)
	ErrorCode_E_EXECUTION_ERROR       ErrorCode = ErrorCode(graph.ErrorCode_E_EXECUTION_ERROR)
	ErrorCode_E_STATEMENT_EMPTY       ErrorCode = ErrorCode(graph.ErrorCode_E_STATEMENT_EMPTY)
	ErrorCode_E_USER_NOT_FOUND        ErrorCode = ErrorCode(graph.ErrorCode_E_USER_NOT_FOUND)
	ErrorCode_E_BAD_PERMISSION        ErrorCode = ErrorCode(graph.ErrorCode_E_BAD_PERMISSION)
	ErrorCode_E_SEMANTIC_ERROR        ErrorCode = ErrorCode(graph.ErrorCode_E_SEMANTIC_ERROR)
	ErrorCode_E_PARTIAL_SUCCEEDED     ErrorCode = ErrorCode(graph.ErrorCode_E_PARTIAL_SUCCEEDED)
)

func genResultSet(resp *graph.ExecutionResponse) *ResultSet {
	var colNames []string
	var colNameIndexMap = make(map[string]int)

	if resp.Data == nil { // if resp.Data != nil then resp.Data.row and resp.Data.colNames wont be nil
		return &ResultSet{
			resp:            resp,
			columnNames:     colNames,
			colNameIndexMap: colNameIndexMap,
		}
	}
	for i, name := range resp.Data.ColumnNames {
		colNames = append(colNames, string(name))
		colNameIndexMap[string(name)] = i
	}

	return &ResultSet{
		resp:            resp,
		columnNames:     colNames,
		colNameIndexMap: colNameIndexMap,
	}
}

func genValWarps(row *nebula.Row) ([]*ValueWrapper, error) {
	if row == nil {
		return nil, fmt.Errorf("Failed to generate valueWrapper: invalid row")
	}
	var valWraps []*ValueWrapper
	for _, val := range row.Values {
		if val == nil {
			return nil, fmt.Errorf("Failed to generate valueWrapper: value is nil")
		}
		valWraps = append(valWraps, &ValueWrapper{val})
	}
	return valWraps, nil
}

func genNode(vertex *nebula.Vertex) (*Node, error) {
	if vertex == nil {
		return nil, fmt.Errorf("Failed to generate Node: invalid vertex")
	}
	var tags []string
	nameIndex := make(map[string]int)

	// Iterate through all tags of the vertex
	for i, tag := range vertex.GetTags() {
		name := string(tag.Name)
		// Get tags
		tags = append(tags, name)
		nameIndex[name] = i
	}

	return &Node{
		vertex:          vertex,
		tags:            tags,
		tagNameIndexMap: nameIndex,
	}, nil
}

func genRelationship(edge *nebula.Edge) (*Relationship, error) {
	if edge == nil {
		return nil, fmt.Errorf("Failed to generate Relationship: invalid edge")
	}
	return &Relationship{
		edge: edge,
	}, nil
}

func genPathWrapper(path *nebula.Path) (*PathWrapper, error) {
	if path == nil {
		return nil, fmt.Errorf("Failed to generate Path Wrapper: invalid path")
	}
	var (
		nodeList         []*Node
		relationshipList []*Relationship
		segList          []segment
		edge             *nebula.Edge
		segStartNode     *Node
		segEndNode       *Node
		segType          nebula.EdgeType
	)
	src, err := genNode(path.Src)
	if err != nil {
		return nil, err
	}
	nodeList = append(nodeList, src)

	for _, step := range path.Steps {
		dst, err := genNode(step.Dst)
		if err != nil {
			return nil, err
		}
		nodeList = append(nodeList, dst)
		// determine direction
		stepType := step.Type
		if stepType > 0 {
			segStartNode = src
			segEndNode = dst
			segType = stepType
		} else {
			segStartNode = dst // switch with src
			segEndNode = src
			segType = -stepType
		}
		edge = &nebula.Edge{
			Src:     segStartNode.getRawID(),
			Dst:     segEndNode.getRawID(),
			Type:    segType,
			Name:    step.Name,
			Ranking: step.Ranking,
			Props:   step.Props,
		}
		relationship, err := genRelationship(edge)
		if err != nil {
			return nil, err
		}
		relationshipList = append(relationshipList, relationship)

		// Check segments
		if len(segList) > 0 {
			prevStart := segList[len(segList)-1].startNode.GetID()
			prevEnd := segList[len(segList)-1].endNode.GetID()
			nextStart := segStartNode.GetID()
			nextEnd := segEndNode.GetID()
			if prevStart.String() != nextStart.String() && prevStart.String() != nextEnd.String() &&
				prevEnd.String() != nextStart.String() && prevEnd.String() != nextEnd.String() {
				return nil, fmt.Errorf("Failed to generate PathWrapper, Path received is invalid")
			}
		}
		segList = append(segList, segment{
			startNode:    segStartNode,
			relationship: relationship,
			endNode:      segEndNode,
		})
		src = dst
	}
	return &PathWrapper{
		path:             path,
		nodeList:         nodeList,
		relationshipList: relationshipList,
		segments:         segList,
	}, nil
}

// Returns ExecutionResponse as a JSON []byte.
// To get the string value in the nested JSON struct, decode with base64
func (res ResultSet) MarshalJSON() ([]byte, error) {
	if res.resp.Data == nil {
		return nil, fmt.Errorf("Failed to generate JSON, DataSet is empty")
	}
	return json.Marshal(res.resp.Data)
}

// Returns a 2D array of strings representing the query result
// If resultSet.resp.data is nil, returns an empty 2D array
func (res ResultSet) AsStringTable() [][]string {
	var resTable [][]string
	colNames := res.GetColNames()
	resTable = append(resTable, colNames)
	rows := res.GetRows()
	for _, row := range rows {
		var tempRow []string
		for _, val := range row.Values {
			tempRow = append(tempRow, ValueWrapper{val}.String())
		}
		resTable = append(resTable, tempRow)
	}
	return resTable
}

// Returns all values in the given column
func (res ResultSet) GetValuesByColName(colName string) ([]*ValueWrapper, error) {
	if !res.hasColName(colName) {
		return nil, fmt.Errorf("Failed to get values, given column name '%s' does not exist", colName)
	}
	// Get index
	index := res.colNameIndexMap[colName]
	var valList []*ValueWrapper
	for _, row := range res.resp.Data.Rows {
		valList = append(valList, &ValueWrapper{row.Values[index]})
	}
	return valList, nil
}

// Returns all values in the row at given index
func (res ResultSet) GetRowValuesByIndex(index int) (*Record, error) {
	if err := checkIndex(index, res.resp.Data.Rows); err != nil {
		return nil, err
	}
	valWrap, err := genValWarps(res.resp.Data.Rows[index])
	if err != nil {
		return nil, err
	}
	return &Record{
		columnNames:     &res.columnNames,
		_record:         valWrap,
		colNameIndexMap: &res.colNameIndexMap,
	}, nil
}

// Returns the number of total rows
func (res ResultSet) GetRowSize() int {
	if res.resp.Data == nil {
		return 0
	}
	return len(res.resp.Data.Rows)
}

// Returns the number of total columns
func (res ResultSet) GetColSize() int {
	if res.resp.Data == nil {
		return 0
	}
	return len(res.resp.Data.ColumnNames)
}

// Returns all rows
func (res ResultSet) GetRows() []*nebula.Row {
	if res.resp.Data == nil {
		var empty []*nebula.Row
		return empty
	}
	return res.resp.Data.Rows
}

func (res ResultSet) GetColNames() []string {
	return res.columnNames
}

// Returns an integer representing an error type
// 0    ErrorCode_SUCCEEDED
// -1   ErrorCode_E_DISCONNECTED
// -2   ErrorCode_E_FAIL_TO_CONNECT
// -3   ErrorCode_E_RPC_FAILURE
// -4   ErrorCode_E_BAD_USERNAME_PASSWORD
// -5   ErrorCode_E_SESSION_INVALID
// -6   ErrorCode_E_SESSION_TIMEOUT
// -7   ErrorCode_E_SYNTAX_ERROR
// -8   ErrorCode_E_EXECUTION_ERROR
// -9   ErrorCode_E_STATEMENT_EMPTY
// -10  ErrorCode_E_USER_NOT_FOUND
// -11  ErrorCode_E_BAD_PERMISSION
// -12  ErrorCode_E_SEMANTIC_ERROR
func (res ResultSet) GetErrorCode() ErrorCode {
	return ErrorCode(int64(res.resp.ErrorCode))
}

func (res ResultSet) GetLatency() int32 {
	return res.resp.LatencyInUs
}

func (res ResultSet) GetSpaceName() string {
	if res.resp.SpaceName == nil {
		return ""
	}
	return string(res.resp.SpaceName)
}

func (res ResultSet) GetErrorMsg() string {
	if res.resp.ErrorMsg == nil {
		return ""
	}
	return string(res.resp.ErrorMsg)
}

func (res ResultSet) IsSetPlanDesc() bool {
	return res.resp.PlanDesc != nil
}

func (res ResultSet) GetPlanDesc() *graph.PlanDescription {
	return res.resp.PlanDesc
}

func (res ResultSet) IsSetComment() bool {
	return res.resp.Comment != nil
}

func (res ResultSet) GetComment() string {
	if res.resp.Comment == nil {
		return ""
	}
	return string(res.resp.Comment)
}

func (res ResultSet) IsSetData() bool {
	return res.resp.Data != nil
}

func (res ResultSet) IsEmpty() bool {
	if !res.IsSetData() || len(res.resp.Data.Rows) == 0 {
		return true
	}
	return false
}

func (res ResultSet) IsSucceed() bool {
	return res.GetErrorCode() == ErrorCode_SUCCEEDED
}

func (res ResultSet) IsPartialSucceed() bool {
	return res.GetErrorCode() == ErrorCode_E_PARTIAL_SUCCEEDED
}

func (res ResultSet) hasColName(colName string) bool {
	if _, ok := res.colNameIndexMap[colName]; ok {
		return true
	}
	return false
}

// Returns value in the record at given column index
func (record Record) GetValueByIndex(index int) (*ValueWrapper, error) {
	if err := checkIndex(index, record._record); err != nil {
		return nil, err
	}
	return record._record[index], nil
}

// Returns value in the record at given column name
func (record Record) GetValueByColName(colName string) (*ValueWrapper, error) {
	if !record.hasColName(colName) {
		return nil, fmt.Errorf("Failed to get values, given column name '%s' does not exist", colName)
	}
	// Get index
	index := (*record.colNameIndexMap)[colName]
	return record._record[index], nil
}

func (record Record) String() string {
	var strList []string
	for _, val := range record._record {
		strList = append(strList, val.String())
	}
	return strings.Join(strList, ", ")
}

func (record Record) hasColName(colName string) bool {
	if _, ok := (*record.colNameIndexMap)[colName]; ok {
		return true
	}
	return false
}

// Returns a list of row vid
func (node Node) getRawID() *nebula.Value {
	return node.vertex.GetVid()
}

// Returns a list of vid of node
func (node Node) GetID() ValueWrapper {
	return ValueWrapper{node.vertex.GetVid()}
}

// Returns a list of tag names of node
func (node Node) GetTags() []string {
	return node.tags
}

// Check if node contains given label
func (node Node) HasTag(label string) bool {
	if _, ok := node.tagNameIndexMap[label]; ok {
		return true
	}
	return false
}

// Returns all properties of a tag
func (node Node) Properties(tagName string) (map[string]*ValueWrapper, error) {
	kvMap := make(map[string]*ValueWrapper)
	// Check if label exists
	if !node.HasTag(tagName) {
		return nil, fmt.Errorf("Failed to get properties: Tag name %s does not exsist in the Node", tagName)
	}
	index := node.tagNameIndexMap[tagName]
	for k, v := range node.vertex.Tags[index].Props {
		kvMap[k] = &ValueWrapper{v}
	}
	return kvMap, nil
}

// Returns all prop names of the given tag name
func (node Node) Keys(tagName string) ([]string, error) {
	if !node.HasTag(tagName) {
		return nil, fmt.Errorf("Failed to get properties: Tag name %s does not exsist in the Node", tagName)
	}
	var propNameList []string
	index := node.tagNameIndexMap[tagName]
	for k, _ := range node.vertex.Tags[index].Props {
		propNameList = append(propNameList, k)
	}
	return propNameList, nil
}

// Returns all prop values of the given tag name
func (node Node) Values(tagName string) ([]*ValueWrapper, error) {
	if !node.HasTag(tagName) {
		return nil, fmt.Errorf("Failed to get properties: Tag name %s does not exsist in the Node", tagName)
	}
	var propValList []*ValueWrapper
	index := node.tagNameIndexMap[tagName]
	for _, v := range node.vertex.Tags[index].Props {
		propValList = append(propValList, &ValueWrapper{v})
	}
	return propValList, nil
}

// Node format: ("VertexID" :tag1{k0: v0,k1: v1}:tag2{k2: v2})
func (node Node) String() string {
	var keyList []string
	var kvStr []string
	var tagStr []string
	vertex := node.vertex
	vid := vertex.GetVid()
	for _, tag := range vertex.GetTags() {
		kvs := tag.GetProps()
		tagName := tag.GetName()
		for k, _ := range kvs {
			keyList = append(keyList, k)
		}
		sort.Strings(keyList)
		for _, k := range keyList {
			kvTemp := fmt.Sprintf("%s: %s", k, ValueWrapper{kvs[k]}.String())
			kvStr = append(kvStr, kvTemp)
		}
		tagStr = append(tagStr, fmt.Sprintf("%s{%s}", tagName, strings.Join(kvStr, ", ")))
		keyList = nil
		kvStr = nil
	}
	if len(tagStr) == 0 { // No tag
		return fmt.Sprintf("(%s)", ValueWrapper{vid}.String())
	}
	return fmt.Sprintf("(%s :%s)", ValueWrapper{vid}.String(), strings.Join(tagStr, " :"))
}

// Returns true if two nodes have same vid
func (n1 Node) IsEqualTo(n2 *Node) bool {
	if n1.GetID().IsString() && n2.GetID().IsString() {
		s1, _ := n1.GetID().AsString()
		s2, _ := n2.GetID().AsString()
		return s1 == s2
	} else if n1.GetID().IsInt() && n2.GetID().IsInt() {
		s1, _ := n1.GetID().AsInt()
		s2, _ := n2.GetID().AsInt()
		return s1 == s2
	}
	return false
}

func (relationship Relationship) GetSrcVertexID() ValueWrapper {
	if relationship.edge.Type > 0 {
		return ValueWrapper{relationship.edge.GetSrc()}
	}
	return ValueWrapper{relationship.edge.GetDst()}
}

func (relationship Relationship) GetDstVertexID() ValueWrapper {
	if relationship.edge.Type > 0 {
		return ValueWrapper{relationship.edge.GetDst()}
	}
	return ValueWrapper{relationship.edge.GetSrc()}
}

func (relationship Relationship) GetEdgeName() string {
	return string(relationship.edge.Name)
}

func (relationship Relationship) GetRanking() int64 {
	return int64(relationship.edge.Ranking)
}

func (relationship Relationship) Properties() map[string]*ValueWrapper {
	kvMap := make(map[string]*ValueWrapper)
	var (
		keyList   []string
		valueList []*ValueWrapper
	)
	for k, v := range relationship.edge.Props {
		keyList = append(keyList, k)
		valueList = append(valueList, &ValueWrapper{v})
	}

	for i := 0; i < len(keyList); i++ {
		kvMap[keyList[i]] = valueList[i]
	}
	return kvMap
}

// Returns a list of keys
func (relationship Relationship) Keys() []string {
	var keys []string
	for key, _ := range relationship.edge.GetProps() {
		keys = append(keys, key)
	}
	return keys
}

// Returns a list of values wrapped as ValueWrappers
func (relationship Relationship) Values() []*ValueWrapper {
	var values []*ValueWrapper
	for _, value := range relationship.edge.GetProps() {
		values = append(values, &ValueWrapper{value})
	}
	return values
}

// Relationship format: [:edge src->dst @ranking {props}]
func (relationship Relationship) String() string {
	edge := relationship.edge
	var keyList []string
	var kvStr []string
	var src string
	var dst string
	for k, _ := range edge.Props {
		keyList = append(keyList, k)
	}
	sort.Strings(keyList)
	for _, k := range keyList {
		kvTemp := fmt.Sprintf("%s: %s", k, ValueWrapper{edge.Props[k]}.String())
		kvStr = append(kvStr, kvTemp)
	}
	if relationship.edge.Type > 0 {
		src = ValueWrapper{edge.Src}.String()
		dst = ValueWrapper{edge.Dst}.String()
	} else {
		src = ValueWrapper{edge.Dst}.String()
		dst = ValueWrapper{edge.Src}.String()
	}
	return fmt.Sprintf(`[:%s %s->%s @%d {%s}]`,
		string(edge.Name), src, dst, edge.Ranking, fmt.Sprintf("%s", strings.Join(kvStr, ", ")))
}

func (r1 Relationship) IsEqualTo(r2 *Relationship) bool {
	if r1.edge.GetSrc().IsSetSVal() && r2.edge.GetSrc().IsSetSVal() &&
		r1.edge.GetDst().IsSetSVal() && r2.edge.GetDst().IsSetSVal() {
		s1, _ := ValueWrapper{r1.edge.GetSrc()}.AsString()
		s2, _ := ValueWrapper{r2.edge.GetSrc()}.AsString()
		return s1 == s2 && string(r1.edge.Name) == string(r2.edge.Name) && r1.edge.Ranking == r2.edge.Ranking
	} else if r1.edge.GetSrc().IsSetIVal() && r2.edge.GetSrc().IsSetIVal() &&
		r1.edge.GetDst().IsSetIVal() && r2.edge.GetDst().IsSetIVal() {
		s1, _ := ValueWrapper{r1.edge.GetSrc()}.AsInt()
		s2, _ := ValueWrapper{r2.edge.GetSrc()}.AsInt()
		return s1 == s2 && string(r1.edge.Name) == string(r2.edge.Name) && r1.edge.Ranking == r2.edge.Ranking
	}
	return false
}

func (path *PathWrapper) GetPathLength() int {
	return len(path.segments)
}

func (path *PathWrapper) GetNodes() []*Node {
	return path.nodeList
}

func (path *PathWrapper) GetRelationships() []*Relationship {
	return path.relationshipList
}

func (path *PathWrapper) GetSegments() []segment {
	return path.segments
}

func (path *PathWrapper) ContainsNode(node Node) bool {
	for _, n := range path.nodeList {
		if n.IsEqualTo(&node) {
			return true
		}
	}
	return false
}

func (path *PathWrapper) ContainsRelationship(relationship *Relationship) bool {
	for _, r := range path.relationshipList {
		if r.IsEqualTo(relationship) {
			return true
		}
	}
	return false
}

func (path *PathWrapper) GetStartNode() (*Node, error) {
	if len(path.segments) == 0 {
		return nil, fmt.Errorf("Failed to get start node, no node in the path")
	}
	return path.segments[0].startNode, nil
}

func (path *PathWrapper) GetEndNode() (*Node, error) {
	if len(path.segments) == 0 {
		return nil, fmt.Errorf("Failed to get start node, no node in the path")
	}
	return path.segments[len(path.segments)-1].endNode, nil
}

// Path format: <("VertexID" :tag1{k0: v0,k1: v1})-[:TypeName@ranking {edgeProps}]->("VertexID2" :tag1{k0: v0,k1: v1} :tag2{k2: v2})-[:TypeName@ranking {edgeProps}]->("VertexID3" :tag1{k0: v0,k1: v1})>
func (pathWrap *PathWrapper) String() string {
	path := pathWrap.path
	src := path.Src
	steps := path.Steps
	resStr := ValueWrapper{&nebula.Value{VVal: src}}.String()
	for _, step := range steps {
		var keyList []string
		var kvStr []string
		for k, _ := range step.Props {
			keyList = append(keyList, k)
		}
		sort.Strings(keyList)
		for _, k := range keyList {
			kvTemp := fmt.Sprintf("%s: %s", k, ValueWrapper{step.Props[k]}.String())
			kvStr = append(kvStr, kvTemp)
		}
		var dirChar1 string
		var dirChar2 string
		if step.Type > 0 {
			dirChar1 = "-"
			dirChar2 = "->"
		} else {
			dirChar1 = "<-"
			dirChar2 = "-"
		}
		resStr = resStr + fmt.Sprintf("%s[:%s@%d {%s}]%s%s",
			dirChar1,
			string(step.Name),
			step.Ranking,
			fmt.Sprintf("%s", strings.Join(kvStr, ", ")),
			dirChar2,
			ValueWrapper{&nebula.Value{VVal: step.Dst}}.String())
	}
	return "<" + resStr + ">"
}

func (p1 *PathWrapper) IsEqualTo(p2 *PathWrapper) bool {
	// Check length
	if len(p1.nodeList) != len(p2.nodeList) || len(p1.relationshipList) != len(p2.relationshipList) ||
		len(p1.segments) != len(p2.segments) {
		return false
	}
	// Check nodes
	for i := 0; i < len(p1.nodeList); i++ {
		if !p1.nodeList[i].IsEqualTo(p2.nodeList[i]) {
			return false
		}
	}
	// Check relationships
	for i := 0; i < len(p1.relationshipList); i++ {
		if !p1.relationshipList[i].IsEqualTo(p2.relationshipList[i]) {
			return false
		}
	}
	// Check segments
	for i := 0; i < len(p1.segments); i++ {
		if !p1.segments[i].startNode.IsEqualTo(p2.segments[i].startNode) ||
			!p1.segments[i].endNode.IsEqualTo(p2.segments[i].endNode) ||
			!p1.segments[i].relationship.IsEqualTo(p2.segments[i].relationship) {
			return false
		}
	}
	return true
}

func checkIndex(index int, list interface{}) error {
	if _, ok := list.([]*nebula.Row); ok {
		if index < 0 || index >= len(list.([]*nebula.Row)) {
			return fmt.Errorf("Failed to get Value, the index is out of range")
		}
		return nil
	} else if _, ok := list.([]*ValueWrapper); ok {
		if index < 0 || index >= len(list.([]*ValueWrapper)) {
			return fmt.Errorf("Failed to get Value, the index is out of range")
		}
		return nil
	}
	return fmt.Errorf("Given list type is invalid")
}

func graphvizString(s string) string {
	s = strings.Replace(s, "{", "\\{", -1)
	s = strings.Replace(s, "}", "\\}", -1)
	s = strings.Replace(s, "\"", "\\\"", -1)
	s = strings.Replace(s, "[", "\\[", -1)
	s = strings.Replace(s, "]", "\\]", -1)
	return s
}

func prettyFormatJsonString(value []byte) string {
	var prettyJson bytes.Buffer
	if err := json.Indent(&prettyJson, value, "", "  "); err != nil {
		return string(value)
	}
	return prettyJson.String()
}

func name(planNodeDesc *graph.PlanNodeDescription) string {
	return fmt.Sprintf("%s_%d", planNodeDesc.GetName(), planNodeDesc.GetId())
}

func condEdgeLabel(condNode *graph.PlanNodeDescription, doBranch bool) string {
	name := strings.ToLower(string(condNode.GetName()))
	if strings.HasPrefix(name, "select") {
		if doBranch {
			return "Y"
		}
		return "N"
	}
	if strings.HasPrefix(name, "loop") {
		if doBranch {
			return "Do"
		}
	}
	return ""
}

func nodeString(planNodeDesc *graph.PlanNodeDescription, planNodeName string) string {
	var outputVar = graphvizString(string(planNodeDesc.GetOutputVar()))
	var inputVar string
	if planNodeDesc.IsSetDescription() {
		desc := planNodeDesc.GetDescription()
		for _, pair := range desc {
			key := string(pair.GetKey())
			if key == "inputVar" {
				inputVar = graphvizString(string(pair.GetValue()))
			}
		}
	}
	return fmt.Sprintf("\t\"%s\"[label=\"{%s|outputVar: %s|inputVar: %s}\", shape=Mrecord];\n",
		planNodeName, planNodeName, outputVar, inputVar)
}

func edgeString(start, end string) string {
	return fmt.Sprintf("\t\"%s\"->\"%s\";\n", start, end)
}

func conditionalEdgeString(start, end, label string) string {
	return fmt.Sprintf("\t\"%s\"->\"%s\"[label=\"%s\", style=dashed];\n", start, end, label)
}

func conditionalNodeString(name string) string {
	return fmt.Sprintf("\t\"%s\"[shape=diamond];\n", name)
}

func nodeById(p *graph.PlanDescription, nodeId int64) *graph.PlanNodeDescription {
	line := p.GetNodeIndexMap()[nodeId]
	return p.GetPlanNodeDescs()[line]
}

func findBranchEndNode(p *graph.PlanDescription, condNodeId int64, isDoBranch bool) int64 {
	for _, node := range p.GetPlanNodeDescs() {
		if node.IsSetBranchInfo() {
			bInfo := node.GetBranchInfo()
			if bInfo.GetConditionNodeID() == condNodeId && bInfo.GetIsDoBranch() == isDoBranch {
				return node.GetId()
			}
		}
	}
	return -1
}

func findFirstStartNodeFrom(p *graph.PlanDescription, nodeId int64) int64 {
	node := nodeById(p, nodeId)
	for {
		deps := node.GetDependencies()
		if len(deps) == 0 {
			if strings.ToLower(string(node.GetName())) != "start" {
				return -1
			}
			return node.GetId()
		}
		node = nodeById(p, deps[0])
	}
}

// explain/profile format="dot"
func (res ResultSet) MakeDotGraph() string {
	p := res.GetPlanDesc()
	planNodeDescs := p.GetPlanNodeDescs()
	var builder strings.Builder
	builder.WriteString("digraph exec_plan {\n")
	builder.WriteString("\trankdir=BT;\n")
	for _, planNodeDesc := range planNodeDescs {
		planNodeName := name(planNodeDesc)
		switch strings.ToLower(string(planNodeDesc.GetName())) {
		case "select":
			builder.WriteString(conditionalNodeString(planNodeName))
			dep := nodeById(p, planNodeDesc.GetDependencies()[0])
			// then branch
			thenNodeId := findBranchEndNode(p, planNodeDesc.GetId(), true)
			builder.WriteString(edgeString(name(nodeById(p, thenNodeId)), name(dep)))
			thenStartId := findFirstStartNodeFrom(p, thenNodeId)
			builder.WriteString(conditionalEdgeString(name(planNodeDesc), name(nodeById(p, thenStartId)), "Y"))
			// else branch
			elseNodeId := findBranchEndNode(p, planNodeDesc.GetId(), false)
			builder.WriteString(edgeString(name(nodeById(p, elseNodeId)), name(dep)))
			elseStartId := findFirstStartNodeFrom(p, elseNodeId)
			builder.WriteString(conditionalEdgeString(name(planNodeDesc), name(nodeById(p, elseStartId)), "N"))
			// dep
			builder.WriteString(edgeString(name(dep), planNodeName))
		case "loop":
			builder.WriteString(conditionalNodeString(planNodeName))
			dep := nodeById(p, planNodeDesc.GetDependencies()[0])
			// do branch
			doNodeId := findBranchEndNode(p, planNodeDesc.GetId(), true)
			builder.WriteString(edgeString(name(nodeById(p, doNodeId)), name(planNodeDesc)))
			doStartId := findFirstStartNodeFrom(p, doNodeId)
			builder.WriteString(conditionalEdgeString(name(planNodeDesc), name(nodeById(p, doStartId)), "Do"))
			// dep
			builder.WriteString(edgeString(name(dep), planNodeName))
		default:
			builder.WriteString(nodeString(planNodeDesc, planNodeName))
			if planNodeDesc.IsSetDependencies() {
				for _, depId := range planNodeDesc.GetDependencies() {
					builder.WriteString(edgeString(name(nodeById(p, depId)), planNodeName))
				}
			}
		}
	}
	builder.WriteString("}")
	return builder.String()
}

// explain/profile format="dot:struct"
func (res ResultSet) MakeDotGraphByStruct() string {
	p := res.GetPlanDesc()
	planNodeDescs := p.GetPlanNodeDescs()
	var builder strings.Builder
	builder.WriteString("digraph exec_plan {\n")
	builder.WriteString("\trankdir=BT;\n")
	for _, planNodeDesc := range planNodeDescs {
		planNodeName := name(planNodeDesc)
		switch strings.ToLower(string(planNodeDesc.GetName())) {
		case "select":
			builder.WriteString(conditionalNodeString(planNodeName))
		case "loop":
			builder.WriteString(conditionalNodeString(planNodeName))
		default:
			builder.WriteString(nodeString(planNodeDesc, planNodeName))
		}

		if planNodeDesc.IsSetDependencies() {
			for _, depId := range planNodeDesc.GetDependencies() {
				dep := nodeById(p, depId)
				builder.WriteString(edgeString(name(dep), planNodeName))
			}
		}

		if planNodeDesc.IsSetBranchInfo() {
			branchInfo := planNodeDesc.GetBranchInfo()
			condNode := nodeById(p, branchInfo.GetConditionNodeID())
			label := condEdgeLabel(condNode, branchInfo.GetIsDoBranch())
			builder.WriteString(conditionalEdgeString(planNodeName, name(condNode), label))
		}
	}
	builder.WriteString("}")
	return builder.String()
}

// explain/profile format="row"
func (res ResultSet) MakePlanByRow() [][]interface{} {
	p := res.GetPlanDesc()
	planNodeDescs := p.GetPlanNodeDescs()
	var rows [][]interface{}
	for _, planNodeDesc := range planNodeDescs {
		var row []interface{}
		row = append(row, planNodeDesc.GetId(), string(planNodeDesc.GetName()))

		if planNodeDesc.IsSetDependencies() {
			var deps []string
			for _, dep := range planNodeDesc.GetDependencies() {
				deps = append(deps, fmt.Sprintf("%d", dep))
			}
			row = append(row, strings.Join(deps, ","))
		} else {
			row = append(row, "")
		}

		if planNodeDesc.IsSetProfiles() {
			var strArr []string
			for i, profile := range planNodeDesc.GetProfiles() {
				otherStats := profile.GetOtherStats()
				if otherStats != nil {
					strArr = append(strArr, "{")
				}
				s := fmt.Sprintf("ver: %d, rows: %d, execTime: %dus, totalTime: %dus",
					i, profile.GetRows(), profile.GetExecDurationInUs(), profile.GetTotalDurationInUs())
				strArr = append(strArr, s)

				for k, v := range otherStats {
					strArr = append(strArr, fmt.Sprintf("%s: %s", k, v))
				}
				if otherStats != nil {
					strArr = append(strArr, "}")
				}
			}
			row = append(row, strings.Join(strArr, "\n"))
		} else {
			row = append(row, "")
		}

		var columnInfo []string
		if planNodeDesc.IsSetBranchInfo() {
			branchInfo := planNodeDesc.GetBranchInfo()
			columnInfo = append(columnInfo, fmt.Sprintf("branch: %t, nodeId: %d\n",
				branchInfo.GetIsDoBranch(), branchInfo.GetConditionNodeID()))
		}

		outputVar := fmt.Sprintf("outputVar: %s", prettyFormatJsonString(planNodeDesc.GetOutputVar()))
		columnInfo = append(columnInfo, outputVar)

		if planNodeDesc.IsSetDescription() {
			desc := planNodeDesc.GetDescription()
			for _, pair := range desc {
				value := prettyFormatJsonString(pair.GetValue())
				columnInfo = append(columnInfo, fmt.Sprintf("%s: %s", string(pair.GetKey()), value))
			}
		}
		row = append(row, strings.Join(columnInfo, "\n"))
		rows = append(rows, row)
	}
	return rows
}
