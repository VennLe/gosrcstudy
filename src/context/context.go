// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package context 定义了 Context 类型，它在 API 边界和进程间传递截止时间、取消信号及其他请求范围的值。
//
// 服务器接收到的请求应创建一个 [Context]，而向服务器发出的调用应接受一个 Context。函数调用链必须在它们之间传播这个 Context，
// 可以选择性地用 [WithCancel]、[WithDeadline]、[WithTimeout] 或 [WithValue] 创建的派生 Context 来替换它。
//
// 可以取消一个 Context 以指示代表其执行的工作应停止。带有截止时间的 Context 会在截止时间过后被取消。
// 当一个 Context 被取消时，所有从它派生的 Context 也会被取消。
//
// [WithCancel]、[WithDeadline] 和 [WithTimeout] 函数接受一个 Context（父 Context）并返回一个派生的 Context（子 Context）和一个
// [CancelFunc]。直接调用 CancelFunc 会取消子 Context 及其子代，移除父 Context 对子 Context 的引用，并停止任何相关的计时器。
// 若不调用 CancelFunc，会导致子 Context 及其子代泄漏，直到父 Context 被取消。go vet 工具会检查是否在所有控制流路径上都使用了 CancelFunc。
//
// [WithCancelCause]、[WithDeadlineCause] 和 [WithTimeoutCause] 函数返回一个 [CancelCauseFunc]，它接受一个错误并将其记录为
// 取消原因。在被取消的 context 或其任何子代上调用 [Cause] 可检索到该原因。如果未指定原因，Cause(ctx) 会返回与 ctx.Err() 相同的值。
//
// 使用 Context 的程序应遵循以下规则，以保持跨包的接口一致性，并使静态分析工具能够检查 context 的传播：
//
// 不要将 Context 存储在结构体类型中；相反，应显式地将 Context 传递给每个需要它的函数。这在
// https://go.dev/blog/context-and-structs 中有进一步讨论。Context 应作为第一个参数，通常命名为 ctx：
//
//	func DoSomething(ctx context.Context, arg Arg) error {
//		// ... 使用 ctx ...
//	}
//
// 不要传递 nil 的 [Context]，即使函数允许这样做。如果你不确定该使用哪个 Context，请传递 [context.TODO]。
//
// 仅将 context 的 Values 用于跨进程和 API 传递的请求范围数据，而不是用于向函数传递可选参数。
//
// 同一个 Context 可以传递给在不同的 goroutine 中运行的函数；Context 可以被多个 goroutine 同时安全地使用。
//
// 有关使用 Context 的服务器的示例代码，请参阅 https://go.dev/blog/context。
package context

import (
	"errors"
	"internal/reflectlite"
	"sync"
	"sync/atomic"
	"time"
)

// Context 携带截止时间、取消信号及其他跨 API 边界的值。
//
// Context 的方法可以被多个 goroutine 同时调用。
type Context interface {
	// Deadline 返回代表此上下文完成的工作应被取消的时间。若未设置截止时间，
	// 则返回 ok==false。连续多次调用 Deadline 会返回相同的结果。
	Deadline() (deadline time.Time, ok bool)

	// Done 返回一个通道，当代表此上下文的工作应被取消时，该通道会被关闭。如果此上下文永不可取消，则 Done 可能返回 nil。连续多次调用 Done 会返回相同的值。
	// Done 通道的关闭可能异步发生，即在 cancel 函数返回之后。
	//
	// WithCancel 安排在 cancel 被调用时关闭 Done；
	// WithDeadline 安排在截止时间到期时关闭 Done；
	// WithTimeout 安排在超时时关闭 Done。
	//
	// Done 可用于 select 语句中：
	//
	//	// Stream 通过 DoSomething 生成值并发送到 out，
	//	// 直到 DoSomething 返回错误或 ctx.Done 被关闭。
	//  func Stream(ctx context.Context, out chan<- Value) error {
	//  	for {
	//  		v, err := DoSomething(ctx)
	//  		if err != nil {
	//  			return err
	//  		}
	//  		select {
	//  		case <-ctx.Done():
	//  			return ctx.Err()
	//  		case out <- v:
	//  		}
	//  	}
	//  }
	//
	// 有关如何使用 Done 通道进行取消的更多示例，请参阅 https://go.dev/blog/pipelines。
	Done() <-chan struct{}

	// 如果 Done 尚未关闭，Err 返回 nil。
	// 如果 Done 已关闭，Err 返回一个非 nil 的错误来解释原因：
	// 如果上下文的截止时间已过，则返回 DeadlineExceeded；
	// 如果上下文因其他原因被取消，则返回 Canceled。
	// 在 Err 返回一个非 nil 的错误后，后续对 Err 的调用都返回相同的错误。
	Err() error

	// Value 返回与此上下文关联的键 key 对应的值，如果没有值与 key 关联，则返回 nil。
	// 使用同一个 key 连续多次调用 Value 会返回相同的结果。
	//
	// 仅将 context 的值用于跨进程和 API 边界传递的请求范围数据，而不要用于向函数传递可选参数。
	//
	// 键（key）用于标识 Context 中的特定值。希望在 Context 中存储值的函数通常会在全局变量中分配一个键，
	// 然后将该键作为参数用于 context.WithValue 和 Context.Value。键可以是任何支持相等性比较的类型；
	// 包应将键定义为非导出的类型，以避免冲突。
	//
	// 定义 Context 键的包应为使用该键存储的值提供类型安全的访问器：
	//
	//		// 包 user 定义了存储在 Context 中的 User 类型。
	// 	package user
	//
	// 	import "context"
	//
	// 	// User 是存储在 Context 中的值的类型。
	// 	type User struct {...}
	//
	// 	// key 是在本包中定义的键的非导出类型。
	//  // 这可以防止与其他包中定义的键发生冲突。
	// 	type key int
	//
	// userKey 是 Context 中 user.User 值的键。它未被导出；
	// 客户端应使用 user.NewContext 和 user.FromContext 而非直接使用此键。
	// 	var userKey key
	//
	// 	// NewContext 返回一个承载了值 u 的新 Context。
	// 	func NewContext(ctx context.Context, u *User) context.Context {
	// 		return context.WithValue(ctx, userKey, u)
	// 	}
	//
	// 	// FromContext 返回存储在 ctx 中的 User 值（如果存在）。
	// 	func FromContext(ctx context.Context) (*User, bool) {
	// 		u, ok := ctx.Value(userKey).(*User)
	// 		return u, ok
	// 	}
	Value(key any) any
}

// Canceled 是当上下文因非截止时间已过的其他原因被取消时，由 [Context.Err] 返回的错误。
var Canceled = errors.New("context canceled")

// DeadlineExceeded 是当上下文因其截止时间已过而被取消时，由 [Context.Err] 返回的错误。
var DeadlineExceeded error = deadlineExceededError{}

type deadlineExceededError struct{}

func (deadlineExceededError) Error() string   { return "context deadline exceeded" }
func (deadlineExceededError) Timeout() bool   { return true }
func (deadlineExceededError) Temporary() bool { return true }

// emptyCtx 永远不会被取消，不存储任何值，也没有截止时间。它是 backgroundCtx 和 todoCtx 的共同基类。
type emptyCtx struct{}

func (emptyCtx) Deadline() (deadline time.Time, ok bool) {
	return
}

func (emptyCtx) Done() <-chan struct{} {
	return nil
}

func (emptyCtx) Err() error {
	return nil
}

func (emptyCtx) Value(key any) any {
	return nil
}

type backgroundCtx struct{ emptyCtx }

func (backgroundCtx) String() string {
	return "context.Background"
}

type todoCtx struct{ emptyCtx }

func (todoCtx) String() string {
	return "context.TODO"
}

// Background 返回一个非 nil 的、空的 [Context]。它永远不会被取消，不存储任何值，也没有截止时间。
// 它通常被用于主函数、初始化和测试，并作为传入请求的顶级 Context。
func Background() Context {
	return backgroundCtx{}
}

// TODO 返回一个非 nil 的、空的 [Context]。当不清楚应使用哪个 Context 或 Context 尚不可用
// （因为周围的函数尚未扩展为接受 Context 参数）时，代码应使用 context.TODO。
func TODO() Context {
	return todoCtx{}
}

// CancelFunc 告知操作应放弃其工作。
// CancelFunc 不会等待工作停止。
// 多个 goroutine 可以同时调用同一个 CancelFunc。
// 首次调用后，后续对 CancelFunc 的调用不会产生任何效果。
type CancelFunc func()

// WithCancel 返回一个派生的上下文，它指向父级上下文但拥有一个新的 Done 通道。当返回的取消函数被调用，
// 或者父级上下文的 Done 通道被关闭时（以先发生者为准），返回的上下文的 Done 通道会被关闭。
//
// 取消此上下文会释放与其关联的资源，因此代码应在此 [Context] 中运行的操作完成后尽快调用 cancel。
func WithCancel(parent Context) (ctx Context, cancel CancelFunc) {
	c := withCancel(parent)
	return c, func() { c.cancel(true, Canceled, nil) }
}

// CancelCauseFunc 的行为类似于 [CancelFunc]，但额外设置了取消原因。这个原因可以通过在已取消的 Context 或其任何派生 Context 上调用 [Cause] 来获取。
//
// 如果上下文已被取消，CancelCauseFunc 不会设置原因。例如，如果 childContext 是从 parentContext 派生的：
//   - 如果在 childContext 因 cause2 被取消之前，parentContext 已因 cause1 被取消，
//     则 Cause(parentContext)  Cause(childContext)  cause1
//   - 如果在 parentContext 因 cause1 被取消之前，childContext 已因 cause2 被取消，
//     则 Cause(parentContext)  cause1 且 Cause(childContext)  cause2
type CancelCauseFunc func(cause error)

// WithCancelCause 的行为类似于 [WithCancel]，但它返回一个 [CancelCauseFunc] 而不是 [CancelFunc]。
// 用一个非 nil 的错误（即“原因”）调用 cancel 会将该错误记录在 ctx 中；之后可以使用 Cause(ctx) 来获取这个错误。
// 用 nil 调用 cancel 会将原因设置为 Canceled。
//
// 使用示例：
//
//	ctx, cancel := context.WithCancelCause(parent)
//	cancel(myError)
//	ctx.Err() // 返回 context.Canceled
//	context.Cause(ctx) // 返回 myError
func WithCancelCause(parent Context) (ctx Context, cancel CancelCauseFunc) {
	c := withCancel(parent)
	return c, func(cause error) { c.cancel(true, Canceled, cause) }
}

func withCancel(parent Context) *cancelCtx {
	if parent == nil {
		panic("cannot create context from nil parent")
	}
	c := &cancelCtx{}
	c.propagateCancel(parent, c)
	return c
}

// Cause 返回一个非 nil 的错误，用于解释 c 被取消的原因。
// c 或其任意父级上下文的首次取消会设置这个原因。
// 如果该取消是通过调用 CancelCauseFunc(err) 发起的，则 [Cause] 返回 err。
// 否则，Cause(c) 返回与 c.Err() 相同的值。
// 如果 c 尚未被取消，Cause 返回 nil。
func Cause(c Context) error {
	err := c.Err()
	if err == nil {
		return nil
	}
	if cc, ok := c.Value(&cancelCtxKey).(*cancelCtx); ok {
		cc.mu.Lock()
		cause := cc.cause
		cc.mu.Unlock()
		if cause != nil {
			return cause
		}
		// 父级 cancelCtx 没有设置原因，因此 c 必然是在某个自定义的上下文实现中被取消的。
	}
	// 我们没有从父级 cancelCtx 得到可返回的原因，因此返回此上下文自身的错误。
	return err
}

// AfterFunc 安排在其自身的 goroutine 中，在 ctx 被取消后调用函数 f。
// 如果 ctx 已被取消，AfterFunc 会立即在其自身的 goroutine 中调用 f。
//
// 在同一个上下文中多次调用 AfterFunc 是独立操作的；一次调用不会替换另一次。
//
// 调用返回的 stop 函数会解除 ctx 与 f 的关联。如果调用成功阻止了 f 的执行，则返回 true。
// 如果 stop 返回 false，可能是因为上下文已被取消且 f 已在其自身的 goroutine 中启动，或者 f 已经被停止。
// stop 函数不会等待 f 完成就返回。如果调用者需要知道 f 是否已完成，它必须与 f 进行显式协调。
//
// 如果 ctx 有 "AfterFunc(func()) func() bool" 方法，AfterFunc 会使用它来调度调用。
func AfterFunc(ctx Context, f func()) (stop func() bool) {
	a := &afterFuncCtx{
		f: f,
	}
	a.cancelCtx.propagateCancel(ctx, a)
	return func() bool {
		stopped := false
		a.once.Do(func() {
			stopped = true
		})
		if stopped {
			a.cancel(true, Canceled, nil)
		}
		return stopped
	}
}

type afterFuncer interface {
	AfterFunc(func()) func() bool
}

type afterFuncCtx struct {
	cancelCtx
	once sync.Once // either starts running f or stops f from running
	f    func()
}

func (a *afterFuncCtx) cancel(removeFromParent bool, err, cause error) {
	a.cancelCtx.cancel(false, err, cause)
	if removeFromParent {
		removeChild(a.Context, a)
	}
	a.once.Do(func() {
		go a.f()
	})
}

// stopCtx 用作 cancelCtx 的父级上下文，当 AfterFunc 已向其父级注册时使用。它持有用于注销 AfterFunc 的 stop 函数。
type stopCtx struct {
	Context
	stop func() bool
}

// goroutines 统计了曾经创建过的 goroutine 数量；用于测试。
var goroutines atomic.Int32

// &cancelCtxKey 是 cancelCtx 返回自身时使用的键。
var cancelCtxKey int

// parentCancelCtx 返回 parent 底层的 *cancelCtx。
// 它通过查找 parent.Value(&cancelCtxKey) 来找到最内层包裹的 cancelCtx，
// 然后检查 parent.Done() 是否与该 cancelCtx 匹配。（如果不匹配，说明 *cancelCtx
// 被包装在一个提供不同 done 通道的自定义实现中，此时我们不应绕过它。）
func parentCancelCtx(parent Context) (*cancelCtx, bool) {
	done := parent.Done()
	if done == closedchan || done == nil {
		return nil, false
	}
	p, ok := parent.Value(&cancelCtxKey).(*cancelCtx)
	if !ok {
		return nil, false
	}
	pdone, _ := p.done.Load().(chan struct{})
	if pdone != done {
		return nil, false
	}
	return p, true
}

// removeChild 从其父级中移除一个上下文。
func removeChild(parent Context, child canceler) {
	if s, ok := parent.(stopCtx); ok {
		s.stop()
		return
	}
	p, ok := parentCancelCtx(parent)
	if !ok {
		return
	}
	p.mu.Lock()
	if p.children != nil {
		delete(p.children, child)
	}
	p.mu.Unlock()
}

// canceler 是一种可以直接取消的上下文类型。其实现是 cancelCtx 和 timerCtx。
type canceler interface {
	cancel(removeFromParent bool, err, cause error)
	Done() <-chan struct{}
}

// closedchan 是一个可复用的、已关闭的通道。
var closedchan = make(chan struct{})

func init() {
	close(closedchan)
}

// cancelCtx 可以被取消。当被取消时，它也会取消任何实现了 canceler 接口的子级上下文。
type cancelCtx struct {
	Context

	mu       sync.Mutex            // protects following fields
	done     atomic.Value          // of chan struct{}, created lazily, closed by first cancel call
	children map[canceler]struct{} // set to nil by the first cancel call
	err      atomic.Value          // set to non-nil by the first cancel call
	cause    error                 // set to non-nil by the first cancel call
}

func (c *cancelCtx) Value(key any) any {
	if key == &cancelCtxKey {
		return c
	}
	return value(c.Context, key)
}

func (c *cancelCtx) Done() <-chan struct{} {
	d := c.done.Load()
	if d != nil {
		return d.(chan struct{})
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	d = c.done.Load()
	if d == nil {
		d = make(chan struct{})
		c.done.Store(d)
	}
	return d.(chan struct{})
}

func (c *cancelCtx) Err() error {
	// 原子加载比互斥锁快约 5 倍，这在紧密循环中可能很重要。
	if err := c.err.Load(); err != nil {
		// 确保在返回非 nil 错误之前，done 通道已关闭。
		<-c.Done()
		return err.(error)
	}
	return nil
}

// propagateCancel 安排在父级被取消时也取消子级。它设置 cancelCtx 的父级上下文。
func (c *cancelCtx) propagateCancel(parent Context, child canceler) {
	c.Context = parent

	done := parent.Done()
	if done == nil {
		return // 父级永远不会被取消
	}

	select {
	case <-done:
		// 父级已被取消
		child.cancel(false, parent.Err(), Cause(parent))
		return
	default:
	}

	if p, ok := parentCancelCtx(parent); ok {
		// 父级是 cancelCtx 或派生自 cancelCtx。
		p.mu.Lock()
		if err := p.err.Load(); err != nil {
			// 父级已被取消
			child.cancel(false, err.(error), p.cause)
		} else {
			if p.children == nil {
				p.children = make(map[canceler]struct{})
			}
			p.children[child] = struct{}{}
		}
		p.mu.Unlock()
		return
	}

	if a, ok := parent.(afterFuncer); ok {
		// 父级实现了 AfterFunc 方法。
		c.mu.Lock()
		stop := a.AfterFunc(func() {
			child.cancel(false, parent.Err(), Cause(parent))
		})
		c.Context = stopCtx{
			Context: parent,
			stop:    stop,
		}
		c.mu.Unlock()
		return
	}

	goroutines.Add(1)
	go func() {
		select {
		case <-parent.Done():
			child.cancel(false, parent.Err(), Cause(parent))
		case <-child.Done():
		}
	}()
}

type stringer interface {
	String() string
}

func contextName(c Context) string {
	if s, ok := c.(stringer); ok {
		return s.String()
	}
	return reflectlite.TypeOf(c).String()
}

func (c *cancelCtx) String() string {
	return contextName(c.Context) + ".WithCancel"
}

// cancel 关闭 c.done，取消 c 的每一个子级，并且如果 removeFromParent 为 true，则从父级的子级列表中移除 c。
// 如果这是 c 第一次被取消，cancel 将 c.cause 设置为 cause。
func (c *cancelCtx) cancel(removeFromParent bool, err, cause error) {
	if err == nil {
		panic("context: internal error: missing cancel error")
	}
	if cause == nil {
		cause = err
	}
	c.mu.Lock()
	if c.err.Load() != nil {
		c.mu.Unlock()
		return // already canceled
	}
	c.err.Store(err)
	c.cause = cause
	d, _ := c.done.Load().(chan struct{})
	if d == nil {
		c.done.Store(closedchan)
	} else {
		close(d)
	}
	for child := range c.children {
		// 注意：在持有父级锁的同时获取了子级的锁。
		child.cancel(false, err, cause)
	}
	c.children = nil
	c.mu.Unlock()

	if removeFromParent {
		removeChild(c.Context, c)
	}
}

// WithoutCancel 返回一个指向父级上下文的派生上下文，当父级被取消时，此派生上下文不会被取消。
// 返回的上下文不返回 Deadline 或 Err，其 Done 通道为 nil。
// 在返回的上下文上调用 [Cause] 会返回 nil。
func WithoutCancel(parent Context) Context {
	if parent == nil {
		panic("cannot create context from nil parent")
	}
	return withoutCancelCtx{parent}
}

type withoutCancelCtx struct {
	c Context
}

func (withoutCancelCtx) Deadline() (deadline time.Time, ok bool) {
	return
}

func (withoutCancelCtx) Done() <-chan struct{} {
	return nil
}

func (withoutCancelCtx) Err() error {
	return nil
}

func (c withoutCancelCtx) Value(key any) any {
	return value(c, key)
}

func (c withoutCancelCtx) String() string {
	return contextName(c.c) + ".WithoutCancel"
}

// WithDeadline 返回一个派生的上下文，它指向父级上下文，但将其截止时间调整为不晚于 d。
// 如果父级的截止时间已经早于 d，WithDeadline(parent, d) 在语义上等同于 parent。
// 返回的 [Context.Done] 通道会在截止时间到期、返回的 cancel 函数被调用，
// 或者父级上下文的 Done 通道被关闭时关闭，以先发生者为准。
//
// 取消此上下文会释放与其关联的资源，因此代码应在此 [Context] 中运行的操作完成后尽快调用 cancel。
func WithDeadline(parent Context, d time.Time) (Context, CancelFunc) {
	return WithDeadlineCause(parent, d, nil)
}

// WithDeadlineCause 的行为类似于 [WithDeadline]，但还会在超过截止时间时设置返回的 Context 的原因。返回的 [CancelFunc] 不会设置原因。
func WithDeadlineCause(parent Context, d time.Time, cause error) (Context, CancelFunc) {
	if parent == nil {
		panic("cannot create context from nil parent")
	}
	if cur, ok := parent.Deadline(); ok && cur.Before(d) {
		// The current deadline is already sooner than the new one.
		return WithCancel(parent)
	}
	c := &timerCtx{
		deadline: d,
	}
	c.cancelCtx.propagateCancel(parent, c)
	dur := time.Until(d)
	if dur <= 0 {
		c.cancel(true, DeadlineExceeded, cause) // deadline has already passed
		return c, func() { c.cancel(false, Canceled, nil) }
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err.Load() == nil {
		c.timer = time.AfterFunc(dur, func() {
			c.cancel(true, DeadlineExceeded, cause)
		})
	}
	return c, func() { c.cancel(true, Canceled, nil) }
}

// timerCtx 包含一个计时器和一个截止时间。它内嵌了一个 cancelCtx 来实现 Done 和 Err 方法。
// 它通过停止其计时器，然后委托给 cancelCtx.cancel 来实现取消。
type timerCtx struct {
	cancelCtx
	timer *time.Timer // Under cancelCtx.mu.

	deadline time.Time
}

func (c *timerCtx) Deadline() (deadline time.Time, ok bool) {
	return c.deadline, true
}

func (c *timerCtx) String() string {
	return contextName(c.cancelCtx.Context) + ".WithDeadline(" +
		c.deadline.String() + " [" +
		time.Until(c.deadline).String() + "])"
}

func (c *timerCtx) cancel(removeFromParent bool, err, cause error) {
	c.cancelCtx.cancel(false, err, cause)
	if removeFromParent {
		// Remove this timerCtx from its parent cancelCtx's children.
		removeChild(c.cancelCtx.Context, c)
	}
	c.mu.Lock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.mu.Unlock()
}

// WithTimeout 等价于调用 WithDeadline(parent, time.Now().Add(timeout))。
//
// 取消此上下文会释放与其关联的资源，因此代码应在此 [Context] 中运行的操作完成后尽快调用 cancel：
//
//	func slowOperationWithTimeout(ctx context.Context) (Result, error) {
//		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
//		defer cancel()  // 如果 slowOperation 在超时前完成，则释放资源
//		return slowOperation(ctx)
//	}
func WithTimeout(parent Context, timeout time.Duration) (Context, CancelFunc) {
	return WithDeadline(parent, time.Now().Add(timeout))
}

// WithTimeoutCause 的行为类似于 [WithTimeout]，但还会在超时时设置返回的 Context 的原因。返回的 [CancelFunc] 不会设置原因。
func WithTimeoutCause(parent Context, timeout time.Duration, cause error) (Context, CancelFunc) {
	return WithDeadlineCause(parent, time.Now().Add(timeout), cause)
}

// WithValue 返回一个指向父级 Context 的派生上下文。在此派生上下文中，与 key 关联的值为 val。
//
// 仅将 context Values 用于跨进程和 API 传递的请求范围数据，不要用于向函数传递可选参数。
//
// 提供的 key 必须是可比较的，并且不应是 string 类型或任何其他内置类型，
// 以避免使用 context 的不同包之间发生冲突。WithValue 的用户应为键定义自己的类型。
// 为避免在分配给 interface{} 时产生分配，context 的键通常具有具体类型 struct{}。
// 或者，导出的 context 键变量的静态类型应为指针或接口类型。
func WithValue(parent Context, key, val any) Context {
	if parent == nil {
		panic("cannot create context from nil parent")
	}
	if key == nil {
		panic("nil key")
	}
	if !reflectlite.TypeOf(key).Comparable() {
		panic("key is not comparable")
	}
	return &valueCtx{parent, key, val}
}

// valueCtx 携带一个键值对。它为该键实现 Value 方法，并将所有其他调用委托给其内嵌的 Context。
type valueCtx struct {
	Context
	key, val any
}

// stringify 尝试以一定方式将 v 字符串化，而不使用 fmt 包，因为我们不希望 context 依赖于 unicode 表。这仅在 *valueCtx.String() 中使用。
func stringify(v any) string {
	switch s := v.(type) {
	case stringer:
		return s.String()
	case string:
		return s
	case nil:
		return "<nil>"
	}
	return reflectlite.TypeOf(v).String()
}

func (c *valueCtx) String() string {
	return contextName(c.Context) + ".WithValue(" +
		stringify(c.key) + ", " +
		stringify(c.val) + ")"
}

func (c *valueCtx) Value(key any) any {
	if c.key == key {
		return c.val
	}
	return value(c.Context, key)
}

func value(c Context, key any) any {
	for {
		switch ctx := c.(type) {
		case *valueCtx:
			if key == ctx.key {
				return ctx.val
			}
			c = ctx.Context
		case *cancelCtx:
			if key == &cancelCtxKey {
				return c
			}
			c = ctx.Context
		case withoutCancelCtx:
			if key == &cancelCtxKey {
				// 当 ctx 是使用 WithoutCancel 创建时，这实现了 Cause(ctx) == nil 的效果。
				return nil
			}
			c = ctx.c
		case *timerCtx:
			if key == &cancelCtxKey {
				return &ctx.cancelCtx
			}
			c = ctx.Context
		case backgroundCtx, todoCtx:
			return nil
		default:
			return c.Value(key)
		}
	}
}
