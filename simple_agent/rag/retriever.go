// Package rag 提供了简单的检索增强生成（RAG）实现。
//
// RAG 的核心思路：在将用户问题发送给 LLM 之前，
// 先从知识库中检索相关内容，注入到 prompt 中，让 LLM 基于检索结果回答。
//
// 本实现特点：
//   - 零外部依赖，使用简单的 TF-IDF 算法
//   - 无需 embedding 服务，使用词频统计
//   - 适合快速原型和中小规模知识库
//
// 使用示例：
//
//	// 1. 创建 RAG
//	r := rag.New(rag.Config{
//	    ChunkSize:   500,    // 每块最多 500 字符
//	    ChunkOverlap: 50,    // 块之间重叠 50 字符
//	    TopK:        3,      // 检索前 3 个最相关的块
//	})
//
//	// 2. 添加文档
//	r.AddDocument("doc1", "北京是中国的首都，人口约 2100 万。")
//	r.AddDocument("doc2", "上海是中国的经济中心，人口约 2400 万。")
//
//	// 3. 检索相关内容
//	results, err := r.Retrieve(ctx, "北京有多少人口？")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// 4. 将检索结果注入 prompt
//	context := r.BuildContext(results)
//	answer, err := simple_agent.Ask(ctx, p, "gpt-4o", context + "\n\n问题：" + question)
package rag

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// ────────────────── 类型定义 ──────────────────

// Document 表示一个文档。
type Document struct {
	ID       string
	Content  string
	Metadata map[string]string
}

// Chunk 表示文档的一个分块。
type Chunk struct {
	DocumentID string
	Content    string
	Index      int
	// TF-IDF 向量（稀疏表示：term → weight）
	Vector map[string]float64
}

// RetrievalResult 检索结果。
type RetrievalResult struct {
	Chunk    Chunk
	Score    float64 // 相关度分数
	Document Document
}

// Config RAG 配置。
type Config struct {
	// ChunkSize 每个文本块的最大字符数。
	// 默认 500，范围建议 200-1000。
	ChunkSize int

	// ChunkOverlap 相邻块之间的重叠字符数。
	// 默认 50，范围建议 0-200。
	// 重叠有助于保持上下文连续性。
	ChunkOverlap int

	// TopK 检索时返回最相关的 K 个块。
	// 默认 3。
	TopK int

	// MinScore 最小相关度阈值，低于此分数的结果被过滤。
	// 默认 0.0（不过滤）。
	MinScore float64
}

// ────────────────── RAG 引擎 ──────────────────

// RAG 是检索增强生成引擎。
type RAG struct {
	config    Config
	documents map[string]Document
	chunks    []Chunk
	idf       map[string]float64 // 逆文档频率
	stopWords map[string]bool
}

// New 创建 RAG 引擎。
func New(cfg Config) *RAG {
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = 500
	}
	if cfg.ChunkOverlap < 0 {
		cfg.ChunkOverlap = 50
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 3
	}
	if cfg.ChunkOverlap >= cfg.ChunkSize {
		cfg.ChunkOverlap = cfg.ChunkSize / 4
	}

	return &RAG{
		config:    cfg,
		documents: make(map[string]Document),
		chunks:    make([]Chunk, 0),
		idf:       make(map[string]float64),
		stopWords: buildStopWords(),
	}
}

// ────────────────── 文档管理 ──────────────────

// AddDocument 添加文档到知识库。
func (r *RAG) AddDocument(id, content string) {
	r.AddDocumentWithMeta(id, content, nil)
}

// AddDocumentWithMeta 添加带元数据的文档。
func (r *RAG) AddDocumentWithMeta(id, content string, metadata map[string]string) {
	doc := Document{
		ID:       id,
		Content:  content,
		Metadata: metadata,
	}
	if doc.Metadata == nil {
		doc.Metadata = make(map[string]string)
	}
	r.documents[id] = doc
	r.chunkDocument(doc)
	r.computeIDF()
}

// RemoveDocument 从知识库移除文档。
func (r *RAG) RemoveDocument(id string) {
	delete(r.documents, id)
	r.reindex()
}

// reindex 重新索引所有文档。
func (r *RAG) reindex() {
	r.chunks = r.chunks[:0]
	for _, doc := range r.documents {
		r.chunkDocument(doc)
	}
	r.computeIDF()
}

// DocumentCount 返回知识库中文档数量。
func (r *RAG) DocumentCount() int {
	return len(r.documents)
}

// ChunkCount 返回知识库中块数量。
func (r *RAG) ChunkCount() int {
	return len(r.chunks)
}

// ────────────────── 分块 ──────────────────

// chunkDocument 将文档分块。
func (r *RAG) chunkDocument(doc Document) {
	text := doc.Content
	if len(text) == 0 {
		return
	}

	chunkSize := r.config.ChunkSize
	overlap := r.config.ChunkOverlap

	// 按段落分割
	paragraphs := strings.Split(text, "\n\n")
	var currentChunk strings.Builder
	var chunkIndex int

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		// 如果单个段落超过 chunkSize，按句子分割
		if len(para) > chunkSize {
			if currentChunk.Len() > 0 {
				r.addChunk(doc.ID, currentChunk.String(), chunkIndex)
				chunkIndex++
				currentChunk.Reset()
			}
			sentences := splitSentences(para)
			for _, sent := range sentences {
				if currentChunk.Len()+len(sent) > chunkSize && currentChunk.Len() > 0 {
					r.addChunk(doc.ID, currentChunk.String(), chunkIndex)
					chunkIndex++
					// 保留重叠部分
					text := currentChunk.String()
					if len(text) > overlap {
						currentChunk.Reset()
						currentChunk.WriteString(text[len(text)-overlap:])
						currentChunk.WriteString(" ")
					} else {
						currentChunk.Reset()
					}
				}
				currentChunk.WriteString(sent)
				currentChunk.WriteString(" ")
			}
		} else if currentChunk.Len()+len(para) > chunkSize && currentChunk.Len() > 0 {
			// 当前块已满，保存并开始新块
			r.addChunk(doc.ID, currentChunk.String(), chunkIndex)
			chunkIndex++
			currentChunk.Reset()
			currentChunk.WriteString(para)
		} else {
			if currentChunk.Len() > 0 {
				currentChunk.WriteString("\n\n")
			}
			currentChunk.WriteString(para)
		}
	}

	// 保存最后一个块
	if currentChunk.Len() > 0 {
		r.addChunk(doc.ID, currentChunk.String(), chunkIndex)
	}
}

// addChunk 添加一个文本块。
func (r *RAG) addChunk(docID, content string, index int) {
	chunk := Chunk{
		DocumentID: docID,
		Content:    content,
		Index:      index,
		Vector:     r.computeTF(content),
	}
	r.chunks = append(r.chunks, chunk)
}

// ────────────────── TF-IDF ──────────────────

// computeTF 计算词频向量。
func (r *RAG) computeTF(text string) map[string]float64 {
	words := tokenize(text)
	if len(words) == 0 {
		return nil
	}

	tf := make(map[string]float64)
	for _, word := range words {
		if !r.stopWords[word] {
			tf[word]++
		}
	}

	// 归一化
	for word := range tf {
		tf[word] /= float64(len(words))
	}

	return tf
}

// computeIDF 计算逆文档频率。
func (r *RAG) computeIDF() {
	totalDocs := float64(len(r.documents))
	if totalDocs == 0 {
		return
	}

	// 重置 IDF
	for k := range r.idf {
		delete(r.idf, k)
	}

	// 统计每个词出现在多少文档中
	docFreq := make(map[string]int)
	for _, chunk := range r.chunks {
		seen := make(map[string]bool)
		for word := range chunk.Vector {
			if !seen[word] {
				docFreq[word]++
				seen[word] = true
			}
		}
	}

	// 计算 IDF: log(N / (1 + df))
	for word, df := range docFreq {
		r.idf[word] = math.Log(totalDocs / (1 + float64(df)))
	}

	// 为所有块的向量计算 TF-IDF
	for i := range r.chunks {
		tfidf := make(map[string]float64)
		for word, tf := range r.chunks[i].Vector {
			if idf, ok := r.idf[word]; ok {
				tfidf[word] = tf * idf
			}
		}
		r.chunks[i].Vector = tfidf
	}
}

// cosineSimilarity 计算两个向量的余弦相似度。
func cosineSimilarity(a, b map[string]float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64
	for word, va := range a {
		if vb, ok := b[word]; ok {
			dotProduct += va * vb
		}
		normA += va * va
	}
	for _, vb := range b {
		normB += vb * vb
	}

	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// ────────────────── 检索 ──────────────────

// Retrieve 检索与查询最相关的块。
func (r *RAG) Retrieve(ctx context.Context, query string) ([]RetrievalResult, error) {
	if len(r.chunks) == 0 {
		return nil, fmt.Errorf("rag: no documents indexed")
	}

	queryVector := r.computeTF(query)
	if len(queryVector) == 0 {
		return nil, fmt.Errorf("rag: query has no valid terms")
	}

	// 计算相似度
	type scoredChunk struct {
		chunk Chunk
		score float64
	}
	var scored []scoredChunk
	for _, chunk := range r.chunks {
		score := cosineSimilarity(queryVector, chunk.Vector)
		if score >= r.config.MinScore {
			scored = append(scored, scoredChunk{chunk: chunk, score: score})
		}
	}

	// 按分数排序
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// 取 top-k
	topK := r.config.TopK
	if topK > len(scored) {
		topK = len(scored)
	}

	results := make([]RetrievalResult, 0, topK)
	for i := 0; i < topK; i++ {
		doc, ok := r.documents[scored[i].chunk.DocumentID]
		if !ok {
			continue
		}
		results = append(results, RetrievalResult{
			Chunk:    scored[i].chunk,
			Score:    scored[i].score,
			Document: doc,
		})
	}

	return results, nil
}

// BuildContext 将检索结果构建为上下文字符串，可直接注入 prompt。
func (r *RAG) BuildContext(results []RetrievalResult) string {
	if len(results) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("【参考信息】\n")

	for i, res := range results {
		builder.WriteString(fmt.Sprintf("--- 来源 %d (文档: %s, 相关度: %.2f) ---\n", i+1, res.Document.ID, res.Score))
		builder.WriteString(res.Chunk.Content)
		builder.WriteString("\n\n")
	}

	builder.WriteString("【以上是检索到的参考信息，请基于以上信息回答问题】\n")
	return builder.String()
}

// ────────────────── 分词 ──────────────────

// tokenize 将文本分词为小写词条。
func tokenize(text string) []string {
	text = strings.ToLower(text)
	words := regexp.MustCompile(`[\w]+`).FindAllString(text, -1)
	if words == nil {
		return nil
	}
	result := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) > 1 && isAlphaNumeric(w) {
			result = append(result, w)
		}
	}
	return result
}

// isAlphaNumeric 检查字符串是否包含字母或数字。
func isAlphaNumeric(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}

// splitSentences 按句号、问号、叹号分割文本。
func splitSentences(text string) []string {
	// 简单实现：按标点分割
	sentences := regexp.MustCompile(`[。！？!?\.]`).Split(text, -1)
	result := make([]string, 0, len(sentences))
	for _, s := range sentences {
		s = strings.TrimSpace(s)
		if s != "" {
			result = append(result, s)
		}
	}
	if len(result) == 0 {
		return []string{text}
	}
	return result
}

// stopWords 常见停用词（中英文）。
var stopWordsList = []string{
	// 英文
	"the", "a", "an", "is", "are", "was", "were", "be", "been",
	"being", "have", "has", "had", "do", "does", "did",
	"will", "would", "could", "should", "may", "might", "shall",
	"can", "need", "to", "of", "in", "for", "on", "with", "at",
	"by", "from", "up", "about", "into", "through", "during",
	"before", "after", "above", "below", "between", "out", "off",
	"over", "under", "again", "further", "then", "once", "here",
	"there", "when", "where", "why", "how", "all", "each",
	"every", "both", "few", "more", "most", "other", "some",
	"such", "no", "nor", "not", "only", "own", "same", "so",
	"than", "too", "very", "just", "because", "as", "until",
	"while", "if", "and", "but", "or", "not",
	// 中文
	"的", "了", "在", "是", "我", "有", "和", "就", "不", "人",
	"都", "一", "一个", "上", "也", "很", "到", "说", "要", "去",
	"你", "会", "着", "没有", "看", "好", "自己", "这", "他", "她",
	"它", "们", "那", "些", "什么", "怎么", "为什么", "如何",
}

func buildStopWords() map[string]bool {
	sw := make(map[string]bool, len(stopWordsList))
	for _, w := range stopWordsList {
		sw[w] = true
	}
	return sw
}
