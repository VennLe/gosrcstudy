// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package template

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sync"
	"text/template"
	"text/template/parse"
)

// Template 是来自 "text/template" 的一个专用模板，用于生成安全的 HTML 文档片段。
type Template struct {
	// 如果转义失败，该错误会“粘附”（被保留）；如果成功，则 escapeOK 为 true。
	escapeErr error
	// 我们本可以直接嵌入 text/template 的字段，但为了安全起见没有这样做，
	// 因为我们需要确保我们自己的命名空间和底层模板的命名空间保持同步。
	text *template.Template
	// 底层模板的解析树，已更新为 HTML 安全的版本。
	Tree       *parse.Tree
	*nameSpace // 所有关联模板共有的部分
}

// escapeOK 是一个哨兵值，用于指示转义操作成功。
var escapeOK = fmt.Errorf("template escaped correctly")

// nameSpace 是在一个关联组中所有模板共享的数据结构。
type nameSpace struct {
	mu      sync.Mutex
	set     map[string]*Template
	escaped bool
	esc     escaper
}

// Templates 返回与模板 t 关联的所有模板（包括 t 本身）组成的切片。
func (t *Template) Templates() []*Template {
	ns := t.nameSpace
	ns.mu.Lock()
	defer ns.mu.Unlock()
	// Return a slice so we don't expose the map.
	m := make([]*Template, 0, len(ns.set))
	for _, v := range ns.set {
		m = append(m, v)
	}
	return m
}

// Option 为模板设置选项。选项由字符串描述，可以是简单字符串或 "key=value" 形式。一个选项字符串中最多只能包含一个等号。如果选项字符串无法识别或无效，Option 方法会引发 panic。
// 已知选项：
// missingkey：控制在执行过程中，如果使用一个不存在于 map 中的键进行索引时的行为。
//
//	"missingkey=default" 或 "missingkey=invalid"
//		默认行为：不执行任何操作，继续执行。
//		如果将其打印输出，索引操作的结果将是字符串 "<no value>"。
//	"missingkey=zero"
//		该操作会返回 map 元素类型的零值。
//
// "missingkey=error"
//
//	执行会立即停止，并返回一个错误。
func (t *Template) Option(opt ...string) *Template {
	t.text.Option(opt...)
	return t
}

// checkCanParse 检查当前是否可以解析模板。如果不可用，则返回一个错误。
func (t *Template) checkCanParse() error {
	if t == nil {
		return nil
	} // checkCanParse 检查当前是否可以解析模板。如果不可用，则返回一个错误。
	t.nameSpace.mu.Lock()
	defer t.nameSpace.mu.Unlock()
	if t.nameSpace.escaped {
		return fmt.Errorf("html/template: cannot Parse after Execute")
	}
	return nil
}

// escape 对所有关联的模板进行转义处理。
func (t *Template) escape() error {
	t.nameSpace.mu.Lock()
	defer t.nameSpace.mu.Unlock()
	t.nameSpace.escaped = true
	if t.escapeErr == nil {
		if t.Tree == nil {
			return fmt.Errorf("template: %q is an incomplete or empty template", t.Name())
		}
		if err := escapeTemplate(t, t.text.Root, t.Name()); err != nil {
			return err
		}
	} else if t.escapeErr != escapeOK {
		return t.escapeErr
	}
	return nil
}

// Execute 将已解析的模板应用于指定的数据对象，并将输出写入 wr。
// 如果在执行模板或写入输出时发生错误，执行会停止，但可能已有部分结果被写入输出 writer。
// 模板可以安全地并行执行，但如果并行执行共享同一个 Writer，输出可能会交错在一起。
func (t *Template) Execute(wr io.Writer, data any) error {
	if err := t.escape(); err != nil {
		return err
	}
	return t.text.Execute(wr, data)
}

// ExecuteTemplate 将关联到 t 且具有给定名称的模板应用于指定的数据对象，并将输出写入 wr。
// 如果在执行模板或写入输出时发生错误，执行会停止，但可能已有部分结果被写入输出 writer。
// 模板可以安全地并行执行，但如果并行执行共享同一个 Writer，输出可能会交错在一起。
func (t *Template) ExecuteTemplate(wr io.Writer, name string, data any) error {
	tmpl, err := t.lookupAndEscapeTemplate(name)
	if err != nil {
		return err
	}
	return tmpl.text.Execute(wr, data)
}

// lookupAndEscapeTemplate 确保具有给定名称的模板已被转义，如果无法完成则返回错误。它同时返回该命名模板。
func (t *Template) lookupAndEscapeTemplate(name string) (tmpl *Template, err error) {
	t.nameSpace.mu.Lock()
	defer t.nameSpace.mu.Unlock()
	t.nameSpace.escaped = true
	tmpl = t.set[name]
	if tmpl == nil {
		return nil, fmt.Errorf("html/template: %q is undefined", name)
	}
	if tmpl.escapeErr != nil && tmpl.escapeErr != escapeOK {
		return nil, tmpl.escapeErr
	}
	if tmpl.text.Tree == nil || tmpl.text.Root == nil {
		return nil, fmt.Errorf("html/template: %q is an incomplete template", name)
	}
	if t.text.Lookup(name) == nil {
		panic("html/template internal error: template escaping out of sync")
	}
	if tmpl.escapeErr == nil {
		err = escapeTemplate(tmpl, tmpl.text.Root, name)
	}
	return tmpl, err
}

// DefinedTemplates 返回一个列出所有已定义模板的字符串，前缀为“; defined templates are: ”。
// 如果没有定义任何模板，则返回空字符串。此方法用于生成错误信息。
func (t *Template) DefinedTemplates() string {
	return t.text.DefinedTemplates()
}

// Parse 将文本解析为模板 t 的主体。
// 文本中定义的命名模板（如 {{define ...}} 或 {{block ...}} 语句）会作为与 t 关联的额外模板定义，并从 t 自身的定义中移除。
// 在首次对 t 或任何关联模板使用 [Template.Execute] 之前，可以通过连续调用 Parse 来重新定义模板。
// 如果模板定义的主体仅包含空白字符和注释，则被视为空定义，不会替换现有模板的主体。
// 这允许通过 Parse 添加新的命名模板定义，而不会覆盖主模板的主体。
func (t *Template) Parse(text string) (*Template, error) {
	if err := t.checkCanParse(); err != nil {
		return nil, err
	}

	ret, err := t.text.Parse(text)
	if err != nil {
		return nil, err
	}

	// 通常，所有命名模板都可能已被修改。
	// 此外，可能还定义了一些新的模板。
	// 底层的 template.Template 集合已更新；我们需要同步更新自己的状态。
	t.nameSpace.mu.Lock()
	defer t.nameSpace.mu.Unlock()
	for _, v := range ret.Templates() {
		name := v.Name()
		tmpl := t.set[name]
		if tmpl == nil {
			tmpl = t.new(name)
		}
		tmpl.text = v
		tmpl.Tree = v.Tree
	}
	return t, nil
}

// AddParseTree 使用给定的名称和解析树创建一个新模板，并将其与 t 关联。
// 如果 t 或任何关联模板已被执行过，则返回错误。
func (t *Template) AddParseTree(name string, tree *parse.Tree) (*Template, error) {
	if err := t.checkCanParse(); err != nil {
		return nil, err
	}

	t.nameSpace.mu.Lock()
	defer t.nameSpace.mu.Unlock()
	text, err := t.text.AddParseTree(name, tree)
	if err != nil {
		return nil, err
	}
	ret := &Template{
		nil,
		text,
		text.Tree,
		t.nameSpace,
	}
	t.set[name] = ret
	return ret, nil
}

// Clone 返回模板的一个副本，包括所有关联的模板。实际的表示（如解析树）不会被复制，但关联模板的命名空间会被复制，
// 因此后续在副本上调用 [Template.Parse] 添加的模板只会附加到副本，而不会影响原始模板。[Template.Clone] 可用于准备公共模板，
// 并在克隆后添加变体定义，以将其用于其他模板的变体版本。
// 如果模板 t 已被执行过，则返回错误。
func (t *Template) Clone() (*Template, error) {
	t.nameSpace.mu.Lock()
	defer t.nameSpace.mu.Unlock()
	if t.escapeErr != nil {
		return nil, fmt.Errorf("html/template: cannot Clone %q after it has executed", t.Name())
	}
	textClone, err := t.text.Clone()
	if err != nil {
		return nil, err
	}
	ns := &nameSpace{set: make(map[string]*Template)}
	ns.esc = makeEscaper(ns)
	ret := &Template{
		nil,
		textClone,
		textClone.Tree,
		ns,
	}
	ret.set[ret.Name()] = ret
	for _, x := range textClone.Templates() {
		name := x.Name()
		src := t.set[name]
		if src == nil || src.escapeErr != nil {
			return nil, fmt.Errorf("html/template: cannot Clone %q after it has executed", t.Name())
		}
		x.Tree = x.Tree.Copy()
		ret.set[name] = &Template{
			nil,
			x,
			x.Tree,
			ret.nameSpace,
		}
	}
	// 返回与此模板名称关联的模板对象。
	return ret.set[ret.Name()], nil
}

// New 分配一个具有指定名称的新 HTML 模板。
func New(name string) *Template {
	ns := &nameSpace{set: make(map[string]*Template)}
	ns.esc = makeEscaper(ns)
	tmpl := &Template{
		nil,
		template.New(name),
		nil,
		ns,
	}
	tmpl.set[name] = tmpl
	return tmpl
}

// New 分配一个与给定模板关联且具有相同分隔符的新 HTML 模板。这种关联是可传递的，
// 允许一个模板通过 {{template}} 动作调用另一个模板。
// 如果给定名称的模板已存在，新的 HTML 模板将替换它。已有的模板将被重置并与 t 解除关联。
func (t *Template) New(name string) *Template {
	t.nameSpace.mu.Lock()
	defer t.nameSpace.mu.Unlock()
	return t.new(name)
}

// new 是 New 方法的内部实现，不包含锁保护。
func (t *Template) new(name string) *Template {
	tmpl := &Template{
		nil,
		t.text.New(name),
		nil,
		t.nameSpace,
	}
	if existing, ok := tmpl.set[name]; ok {
		emptyTmpl := New(existing.Name())
		*existing = *emptyTmpl
	}
	tmpl.set[name] = tmpl
	return tmpl
}

// Name 返回模板的名称。
func (t *Template) Name() string {
	return t.text.Name()
}

type FuncMap = template.FuncMap

// Funcs 将参数映射中的元素添加到模板的函数映射中。
// 此方法必须在解析模板之前调用。
// 如果映射中的值不是具有适当返回类型的函数，将会引发 panic。
// 不过，允许覆盖映射中的元素。返回值为模板本身，因此可以进行链式调用。
func (t *Template) Funcs(funcMap FuncMap) *Template {
	t.text.Funcs(funcMap)
	return t
}

// Delims 将动作分隔符设置为指定的字符串，用于后续调用 [Template.Parse]、[ParseFiles] 或 [ParseGlob] 时。
// 嵌套的模板定义会继承此设置。空字符串表示对应的默认分隔符：{{ 或 }}。
// 返回值为模板本身，因此可以进行链式调用。
func (t *Template) Delims(left, right string) *Template {
	t.text.Delims(left, right)
	return t
}

// Lookup 返回与 t 关联的、具有给定名称的模板，如果没有这样的模板则返回 nil。
func (t *Template) Lookup(name string) *Template {
	t.nameSpace.mu.Lock()
	defer t.nameSpace.mu.Unlock()
	return t.set[name]
}

// Must 是一个辅助函数，它包装了对返回 ([*Template], error) 的函数的调用，并在错误非 nil 时引发 panic。它主要用于变量初始化，例如：
// var t = template.Must(template.New("name").Parse("html"))
func Must(t *Template, err error) *Template {
	if err != nil {
		panic(err)
	}
	return t
}

// ParseFiles 创建一个新的 [Template]，并从指定的文件中解析模板定义。返回的模板名称将是第一个文件的（基本）名称，
// 其内容是第一个文件解析后的内容。必须至少指定一个文件。如果发生错误，解析会停止，并返回 nil。
// 当解析不同目录中同名文件时，最后一个被提及的文件将作为结果模板。例如，ParseFiles("a/foo", "b/foo")
// 会将 "b/foo" 存储为名为 "foo" 的模板，而 "a/foo" 将不可用。
func ParseFiles(filenames ...string) (*Template, error) {
	return parseFiles(nil, readFileOS, filenames...)
}

// ParseFiles 解析指定的文件，并将生成的模板与 t 关联。如果发生错误，解析会停止，并返回 nil；否则返回 t。必须至少指定一个文件。
// 当解析不同目录中同名文件时，最后一个被提及的文件将作为结果模板。
// 如果 t 或任何关联模板已被执行过，ParseFiles 会返回一个错误。
func (t *Template) ParseFiles(filenames ...string) (*Template, error) {
	return parseFiles(t, readFileOS, filenames...)
}

// parseFiles 是方法和函数的辅助实现。如果传入的模板为 nil，则用第一个文件创建它。
func parseFiles(t *Template, readFile func(string) (string, []byte, error), filenames ...string) (*Template, error) {
	if err := t.checkCanParse(); err != nil {
		return nil, err
	}

	if len(filenames) == 0 {
		// 这实际上不算问题，但为了保持一致性，仍需处理。
		return nil, fmt.Errorf("html/template: no files named in call to ParseFiles")
	}
	for _, filename := range filenames {
		name, b, err := readFile(filename)
		if err != nil {
			return nil, err
		}
		s := string(b)
		// 如果第一个模板尚未被定义，它将作为返回值，并且我们会在后续的 New 调用中使用它，以将所有模板关联在一起。
		// 此外，如果此文件与 t 具有相同的名称，则该文件将成为 t 的内容，因此像 t, err := New(name).Funcs(xxx).ParseFiles(name)
		// 这样的调用可以正常工作。否则，我们将创建一个与 t 关联的新模板。
		var tmpl *Template
		if t == nil {
			t = New(name)
		}
		if name == t.Name() {
			tmpl = t
		} else {
			tmpl = t.New(name)
		}
		_, err = tmpl.Parse(s)
		if err != nil {
			return nil, err
		}
	}
	return t, nil
}

// ParseGlob 创建一个新的 [Template]，并从模式匹配的文件中解析模板定义。文件匹配遵循 filepath.Match 的语义，
// 且模式必须至少匹配一个文件。返回的模板将具有模式匹配到的第一个文件的（基本）名称和（解析后的）内容。
// ParseGlob 等效于使用模式匹配到的文件列表调用 [ParseFiles]。
// 当解析不同目录中同名文件时，最后一个被提及的文件将作为结果模板。
func ParseGlob(pattern string) (*Template, error) {
	return parseGlob(nil, pattern)
}

// ParseGlob 解析模式匹配的文件中的模板定义，并将生成的模板与 t 关联。文件匹配遵循 filepath.Match 的语义，
// 且模式必须至少匹配一个文件。ParseGlob 等效于使用模式匹配到的文件列表调用 t.ParseFiles。
// 当解析不同目录中同名文件时，最后一个被提及的文件将作为结果模板。
// 如果 t 或任何关联模板已被执行过，ParseGlob 会返回一个错误。
func (t *Template) ParseGlob(pattern string) (*Template, error) {
	return parseGlob(t, pattern)
}

// parseGlob 是函数和方法 ParseGlob 的内部实现。
func parseGlob(t *Template, pattern string) (*Template, error) {
	if err := t.checkCanParse(); err != nil {
		return nil, err
	}
	filenames, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	if len(filenames) == 0 {
		return nil, fmt.Errorf("html/template: pattern matches no files: %#q", pattern)
	}
	return parseFiles(t, readFileOS, filenames...)
}

// IsTrue 判断一个值是否为“真”，即该值是否不为该类型的零值，以及该值是否具有有意义的真值定义。这是 if 等动作所使用的真值定义标准。
func IsTrue(val any) (truth, ok bool) {
	return template.IsTrue(val)
}

// ParseFS 类似于 [ParseFiles] 或 [ParseGlob]，但它从文件系统 fs 中读取，而不是从宿主操作系统的文件系统中读取。
// 它接受一组 glob 模式。
// （请注意，大多数文件名本身就可作为仅匹配自身的 glob 模式。）
func ParseFS(fs fs.FS, patterns ...string) (*Template, error) {
	return parseFS(nil, fs, patterns)
}

// ParseFS 类似于 [Template.ParseFiles] 或 [Template.ParseGlob]，但它从文件系统 fs 中读取，而不是从宿主操作系统的文件系统中读取。
// 它接受一组 glob 模式。
// （请注意，大多数文件名本身就可作为仅匹配自身的 glob 模式。）
func (t *Template) ParseFS(fs fs.FS, patterns ...string) (*Template, error) {
	return parseFS(t, fs, patterns)
}

func parseFS(t *Template, fsys fs.FS, patterns []string) (*Template, error) {
	var filenames []string
	for _, pattern := range patterns {
		list, err := fs.Glob(fsys, pattern)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 {
			return nil, fmt.Errorf("template: pattern matches no files: %#q", pattern)
		}
		filenames = append(filenames, list...)
	}
	return parseFiles(t, readFileFS(fsys), filenames...)
}

func readFileOS(file string) (name string, b []byte, err error) {
	name = filepath.Base(file)
	b, err = os.ReadFile(file)
	return
}

func readFileFS(fsys fs.FS) func(string) (string, []byte, error) {
	return func(file string) (name string, b []byte, err error) {
		name = path.Base(file)
		b, err = fs.ReadFile(fsys, file)
		return
	}
}
