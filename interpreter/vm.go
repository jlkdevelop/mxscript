// vm.go — experimental bytecode VM for the hot paths of MX Script.
// Compiles a useful subset of the AST (arithmetic, comparison, unary,
// `let`, `=`, `if`, `while`) into a flat slice of stack-machine
// instructions, then runs them on a tight loop.
//
// Why a subset? Programs that mix routes / spawn / channels / SSE /
// tool-calling AI / SQLite still want full interpreter semantics
// because those paths fan out into Go-side IO. The compiler refuses
// to lower anything it doesn't fully understand, and the runtime
// falls back to the tree-walker for those programs. The VM is
// purely an optimisation for tight numeric / data-shuffling code.
//
// Today this ships behind `mx run --bytecode` and `mx bench --bytecode`
// (off by default) so users opt in. Once we have full coverage and
// equivalent semantics for every node, --bytecode flips to default.
package interpreter

import (
	"errors"
	"fmt"

	"github.com/jlkdevelop/mxscript/parser"
)

// errBytecodeFallback is returned when the compiler refuses to lower
// a node. Callers fall back to the tree-walker.
var errBytecodeFallback = errors.New("bytecode fallback")

type Op uint8

const (
	OpConst       Op = iota // push consts[arg]
	OpLoadVar               // push env.Get(varNames[arg])
	OpStoreVar              // env.Set(varNames[arg], pop()) — for `let`
	OpAssignVar             // env.Assign(varNames[arg], pop()) — for `=`
	OpAdd                   // a + b (numeric or string concat)
	OpSub                   // a - b
	OpMul                   // a * b
	OpDiv                   // a / b
	OpMod                   // a % b
	OpEq                    // a == b
	OpNeq                   // a != b
	OpLt                    // a < b
	OpGt                    // a > b
	OpLte                   // a <= b
	OpGte                   // a >= b
	OpNot                   // !a
	OpNeg                   // -a
	OpJump                  // pc = arg (absolute)
	OpJumpIfFalse           // pop; if !truthy: pc = arg
	OpPop                   // discard top
	OpReturn                // halt; return top of stack (or null)
	OpCall                  // pop arg values + callee; push call result
	OpGetField              // pop obj; push obj.<consts[arg].String>
	OpMakeArray             // pop arg values; push as KindArray
	OpMakeObject            // pop arg*2 values (key, value pairs); push as KindObject
	OpGetIndex              // pop index; pop target; push target[index]
	OpLength                // pop value; push its length (array len, string len, object key count)
	OpAndJump               // peek; if falsy: pc = arg (leaves value); else pop + continue
	OpOrJump                // peek; if truthy: pc = arg (leaves value); else pop + continue
	OpNullishJump           // peek; if non-null: pc = arg (leaves value); else pop + continue
)

type Instr struct {
	Op  Op
	Arg int32
}

// Compiled is the output of the bytecode compiler. It owns the
// instruction stream plus the constant pool and the variable name
// table the instructions index into.
type Compiled struct {
	Code     []Instr
	Consts   []Value
	VarNames []string

	// loopGen counts loop-temporary variables so two nested `loop`
	// statements don't share the same hidden name.
	loopGen int
}

// CompileExpr lowers a single expression node into a self-contained
// program that pushes the value and returns. Used for ad-hoc
// expression statements at the top level.
func CompileExpr(e parser.Expr) (*Compiled, bool) {
	c := &Compiled{}
	if !c.compileExpr(e) {
		return nil, false
	}
	c.emit(OpReturn, 0)
	return c, true
}

// CompileBlock lowers a list of statements. Used for the body of
// `if`, `while`, and (eventually) functions. Returns (nil, false) if
// any statement uses something the compiler doesn't understand yet.
func CompileBlock(stmts []parser.Stmt) (*Compiled, bool) {
	c := &Compiled{}
	if !c.compileStmts(stmts) {
		return nil, false
	}
	c.emit(OpReturn, 0)
	return c, true
}

func (c *Compiled) emit(op Op, arg int32) int {
	c.Code = append(c.Code, Instr{Op: op, Arg: arg})
	return len(c.Code) - 1
}

func (c *Compiled) here() int32 { return int32(len(c.Code)) }

// patch rewrites the Arg of an already-emitted jump instruction.
// Used to backfill forward jumps once we know the destination.
func (c *Compiled) patch(at int, target int32) {
	c.Code[at].Arg = target
}

func (c *Compiled) addConst(v Value) int32 {
	c.Consts = append(c.Consts, v)
	return int32(len(c.Consts) - 1)
}

func (c *Compiled) addVar(name string) int32 {
	for i, n := range c.VarNames {
		if n == name {
			return int32(i)
		}
	}
	c.VarNames = append(c.VarNames, name)
	return int32(len(c.VarNames) - 1)
}

func (c *Compiled) compileExpr(e parser.Expr) bool {
	switch n := e.(type) {
	case *parser.NumberLit:
		c.emit(OpConst, c.addConst(NumberValue(n.Value)))
		return true
	case *parser.StringLit:
		c.emit(OpConst, c.addConst(StringValue(n.Value)))
		return true
	case *parser.BoolLit:
		c.emit(OpConst, c.addConst(BoolValue(n.Value)))
		return true
	case *parser.NullLit:
		c.emit(OpConst, c.addConst(NullValue()))
		return true
	case *parser.Identifier:
		c.emit(OpLoadVar, c.addVar(n.Name))
		return true
	case *parser.UnaryExpr:
		if !c.compileExpr(n.Operand) {
			return false
		}
		switch n.Op {
		case "-":
			c.emit(OpNeg, 0)
		case "!":
			c.emit(OpNot, 0)
		default:
			return false
		}
		return true
	case *parser.CallExpr:
		// Compile the callee, then each argument left-to-right, then
		// emit OpCall with the argument count. The runtime resolves
		// callee identity (native vs user function) at execution.
		if !c.compileExpr(n.Callee) {
			return false
		}
		for _, a := range n.Args {
			// Spread args would need a different opcode; bail out for
			// now and let the tree-walker handle them.
			if _, isSpread := a.(*parser.SpreadExpr); isSpread {
				return false
			}
			if !c.compileExpr(a) {
				return false
			}
		}
		c.emit(OpCall, int32(len(n.Args)))
		return true
	case *parser.MemberExpr:
		// Support obj.field reads on the VM. Optional chaining (?.)
		// would need a guarded path — fall back to the tree-walker
		// for that case.
		if n.Optional {
			return false
		}
		if !c.compileExpr(n.Object) {
			return false
		}
		c.emit(OpGetField, c.addConst(StringValue(n.Property)))
		return true
	case *parser.IndexExpr:
		if !c.compileExpr(n.Object) {
			return false
		}
		if !c.compileExpr(n.Index) {
			return false
		}
		c.emit(OpGetIndex, 0)
		return true
	case *parser.ArrayLit:
		// Spread inside an array literal needs runtime concat; bail.
		for _, el := range n.Elements {
			if _, isSpread := el.(*parser.SpreadExpr); isSpread {
				return false
			}
		}
		for _, el := range n.Elements {
			if !c.compileExpr(el) {
				return false
			}
		}
		c.emit(OpMakeArray, int32(len(n.Elements)))
		return true
	case *parser.ObjectLit:
		// For each pair: push the key as a constant, then compile
		// the value. OpMakeObject pops 2*N values.
		for _, p := range n.Pairs {
			c.emit(OpConst, c.addConst(StringValue(p.Key)))
			if !c.compileExpr(p.Value) {
				return false
			}
		}
		c.emit(OpMakeObject, int32(len(n.Pairs)))
		return true
	case *parser.BinaryExpr:
		// Short-circuit operators emit a peek-and-jump pattern: the
		// runtime opcode keeps the left value on the stack when the
		// short-circuit condition matches, otherwise pops it before
		// the right operand evaluates.
		if n.Op == "&&" || n.Op == "||" || n.Op == "??" {
			if !c.compileExpr(n.Left) {
				return false
			}
			var op Op
			switch n.Op {
			case "&&":
				op = OpAndJump
			case "||":
				op = OpOrJump
			case "??":
				op = OpNullishJump
			}
			jump := c.emit(op, 0)
			if !c.compileExpr(n.Right) {
				return false
			}
			c.patch(jump, c.here())
			return true
		}
		if !c.compileExpr(n.Left) {
			return false
		}
		if !c.compileExpr(n.Right) {
			return false
		}
		switch n.Op {
		case "+":
			c.emit(OpAdd, 0)
		case "-":
			c.emit(OpSub, 0)
		case "*":
			c.emit(OpMul, 0)
		case "/":
			c.emit(OpDiv, 0)
		case "%":
			c.emit(OpMod, 0)
		case "==":
			c.emit(OpEq, 0)
		case "!=":
			c.emit(OpNeq, 0)
		case "<":
			c.emit(OpLt, 0)
		case ">":
			c.emit(OpGt, 0)
		case "<=":
			c.emit(OpLte, 0)
		case ">=":
			c.emit(OpGte, 0)
		default:
			return false
		}
		return true
	}
	return false
}

func (c *Compiled) compileStmts(stmts []parser.Stmt) bool {
	for _, s := range stmts {
		if !c.compileStmt(s) {
			return false
		}
	}
	return true
}

func (c *Compiled) compileStmt(s parser.Stmt) bool {
	switch n := s.(type) {
	case *parser.LetStmt:
		// Destructuring is too complex for the MVP — fall back.
		if n.Pattern != nil || n.Name == "" {
			return false
		}
		if !c.compileExpr(n.Value) {
			return false
		}
		c.emit(OpStoreVar, c.addVar(n.Name))
		return true

	case *parser.AssignStmt:
		// Only simple identifier targets — `a.b = x` and `a[0] = x` need
		// object/array machinery the VM doesn't have yet.
		ident, ok := n.Target.(*parser.Identifier)
		if !ok {
			return false
		}
		if !c.compileExpr(n.Value) {
			return false
		}
		c.emit(OpAssignVar, c.addVar(ident.Name))
		return true

	case *parser.ExprStmt:
		if !c.compileExpr(n.Expr) {
			return false
		}
		// Expression statements leave a value on the stack; discard it
		// so the stack stays balanced across iterations.
		c.emit(OpPop, 0)
		return true

	case *parser.IfStmt:
		if !c.compileExpr(n.Cond) {
			return false
		}
		jumpToElse := c.emit(OpJumpIfFalse, 0)
		if !c.compileStmts(n.Then) {
			return false
		}
		jumpOverElse := c.emit(OpJump, 0)
		c.patch(jumpToElse, c.here())
		if len(n.Else) > 0 {
			if !c.compileStmts(n.Else) {
				return false
			}
		}
		c.patch(jumpOverElse, c.here())
		return true

	case *parser.WhileStmt:
		loopStart := c.here()
		if !c.compileExpr(n.Cond) {
			return false
		}
		jumpOut := c.emit(OpJumpIfFalse, 0)
		if !c.compileStmts(n.Body) {
			return false
		}
		c.emit(OpJump, loopStart)
		c.patch(jumpOut, c.here())
		return true

	case *parser.LoopStmt:
		// `loop iterable as item { body }` — desugar to:
		//   let __arr_N   = <iterable>
		//   let __len_N   = len(__arr_N)
		//   let __i_N     = 0
		//   while __i_N < __len_N {
		//     let item    = __arr_N[__i_N]
		//     let idxVar? = __i_N
		//     <body>
		//     __i_N = __i_N + 1
		//   }
		// We use unique synthetic names (with `__loop_` prefix) so
		// nested loops don't collide.
		gen := c.loopGen
		c.loopGen++
		arrName := fmt.Sprintf("__loop_arr_%d", gen)
		lenName := fmt.Sprintf("__loop_len_%d", gen)
		idxName := fmt.Sprintf("__loop_idx_%d", gen)

		// __arr = iterable
		if !c.compileExpr(n.Iterable) {
			return false
		}
		c.emit(OpStoreVar, c.addVar(arrName))

		// __len = length(__arr)
		c.emit(OpLoadVar, c.addVar(arrName))
		c.emit(OpLength, 0)
		c.emit(OpStoreVar, c.addVar(lenName))

		// __i = 0
		c.emit(OpConst, c.addConst(NumberValue(0)))
		c.emit(OpStoreVar, c.addVar(idxName))

		loopStart := c.here()
		// while __i < __len
		c.emit(OpLoadVar, c.addVar(idxName))
		c.emit(OpLoadVar, c.addVar(lenName))
		c.emit(OpLt, 0)
		jumpOut := c.emit(OpJumpIfFalse, 0)

		// item = __arr[__i]
		c.emit(OpLoadVar, c.addVar(arrName))
		c.emit(OpLoadVar, c.addVar(idxName))
		c.emit(OpGetIndex, 0)
		c.emit(OpStoreVar, c.addVar(n.Var))

		// optional index variable
		if n.IndexVar != "" {
			c.emit(OpLoadVar, c.addVar(idxName))
			c.emit(OpStoreVar, c.addVar(n.IndexVar))
		}

		if !c.compileStmts(n.Body) {
			return false
		}

		// __i = __i + 1
		c.emit(OpLoadVar, c.addVar(idxName))
		c.emit(OpConst, c.addConst(NumberValue(1)))
		c.emit(OpAdd, 0)
		c.emit(OpStoreVar, c.addVar(idxName))

		c.emit(OpJump, loopStart)
		c.patch(jumpOut, c.here())
		return true

	case *parser.ReturnStmt:
		// `return expr` evaluates the expression and halts the VM
		// with that value on the stack. `return` with no value
		// pushes null. OpReturn is a hard stop, so this terminates
		// the function body even from inside loops or conditionals.
		if n.Value != nil {
			if !c.compileExpr(n.Value) {
				return false
			}
		} else {
			c.emit(OpConst, c.addConst(NullValue()))
		}
		c.emit(OpReturn, 0)
		return true
	}
	return false
}

// Run executes the compiled program against the given environment.
// `interp` is needed for OpCall (function dispatch); pass nil for
// programs that don't include calls (the runtime errors clearly if
// a call is hit without an interpreter).
//
// Variables are read from / written to the env via name lookup, so
// the VM shares state with the tree-walker for free.
func (c *Compiled) Run(interp *Interpreter, env *Env) (Value, error) {
	stack := make([]Value, 0, 32)
	pc := 0
	for pc < len(c.Code) {
		ins := c.Code[pc]
		pc++
		switch ins.Op {
		case OpConst:
			stack = append(stack, c.Consts[ins.Arg])
		case OpLoadVar:
			name := c.VarNames[ins.Arg]
			v, ok := env.Get(name)
			if !ok {
				return Value{}, fmt.Errorf("undefined identifier %q", name)
			}
			stack = append(stack, v)
		case OpStoreVar:
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			env.Set(c.VarNames[ins.Arg], v)
		case OpAssignVar:
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			env.Assign(c.VarNames[ins.Arg], v)
		case OpAdd:
			b := stack[len(stack)-1]
			a := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			if a.Kind == KindNumber && b.Kind == KindNumber {
				stack = append(stack, NumberValue(a.Number+b.Number))
			} else {
				stack = append(stack, StringValue(a.Display()+b.Display()))
			}
		case OpSub:
			b := stack[len(stack)-1]
			a := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			stack = append(stack, NumberValue(a.Number-b.Number))
		case OpMul:
			b := stack[len(stack)-1]
			a := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			stack = append(stack, NumberValue(a.Number*b.Number))
		case OpDiv:
			b := stack[len(stack)-1]
			a := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			if b.Number == 0 {
				return Value{}, fmt.Errorf("division by zero")
			}
			stack = append(stack, NumberValue(a.Number/b.Number))
		case OpMod:
			b := stack[len(stack)-1]
			a := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			if b.Number == 0 {
				return Value{}, fmt.Errorf("modulo by zero")
			}
			stack = append(stack, NumberValue(float64(int64(a.Number)%int64(b.Number))))
		case OpEq:
			b := stack[len(stack)-1]
			a := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			stack = append(stack, BoolValue(valuesEqual(a, b)))
		case OpNeq:
			b := stack[len(stack)-1]
			a := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			stack = append(stack, BoolValue(!valuesEqual(a, b)))
		case OpLt:
			b := stack[len(stack)-1]
			a := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			stack = append(stack, BoolValue(a.Number < b.Number))
		case OpGt:
			b := stack[len(stack)-1]
			a := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			stack = append(stack, BoolValue(a.Number > b.Number))
		case OpLte:
			b := stack[len(stack)-1]
			a := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			stack = append(stack, BoolValue(a.Number <= b.Number))
		case OpGte:
			b := stack[len(stack)-1]
			a := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			stack = append(stack, BoolValue(a.Number >= b.Number))
		case OpNot:
			a := stack[len(stack)-1]
			stack[len(stack)-1] = BoolValue(!a.IsTruthy())
		case OpNeg:
			a := stack[len(stack)-1]
			stack[len(stack)-1] = NumberValue(-a.Number)
		case OpJump:
			pc = int(ins.Arg)
		case OpJumpIfFalse:
			cond := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if !cond.IsTruthy() {
				pc = int(ins.Arg)
			}
		case OpPop:
			stack = stack[:len(stack)-1]
		case OpReturn:
			if len(stack) == 0 {
				return NullValue(), nil
			}
			return stack[len(stack)-1], nil
		case OpCall:
			argc := int(ins.Arg)
			if len(stack) < argc+1 {
				return Value{}, fmt.Errorf("OpCall: stack underflow (have %d, need %d)", len(stack), argc+1)
			}
			args := make([]Value, argc)
			copy(args, stack[len(stack)-argc:])
			callee := stack[len(stack)-argc-1]
			stack = stack[:len(stack)-argc-1]
			if callee.Kind != KindFunction {
				return Value{}, fmt.Errorf("attempt to call %s", callee.typeName())
			}
			fn := callee.Function
			if interp == nil {
				return Value{}, fmt.Errorf("OpCall without an interpreter (cannot dispatch %q)", fn.Name)
			}
			result, err := interp.callFunction(nil, fn, args)
			if err != nil {
				return Value{}, err
			}
			stack = append(stack, result)
		case OpGetField:
			obj := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			field := c.Consts[ins.Arg].String
			switch obj.Kind {
			case KindObject:
				if v, ok := obj.Object.Get(field); ok {
					stack = append(stack, v)
				} else {
					stack = append(stack, NullValue())
				}
			case KindNull:
				stack = append(stack, NullValue())
			default:
				return Value{}, fmt.Errorf("cannot read field %q from %s", field, obj.typeName())
			}
		case OpGetIndex:
			idx := stack[len(stack)-1]
			target := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			switch target.Kind {
			case KindArray:
				if idx.Kind != KindNumber {
					return Value{}, fmt.Errorf("array index must be a number, got %s", idx.typeName())
				}
				k := int(idx.Number)
				if k < 0 || k >= len(target.Array) {
					stack = append(stack, NullValue())
				} else {
					stack = append(stack, target.Array[k])
				}
			case KindObject:
				if idx.Kind != KindString {
					return Value{}, fmt.Errorf("object key must be a string, got %s", idx.typeName())
				}
				if v, ok := target.Object.Get(idx.String); ok {
					stack = append(stack, v)
				} else {
					stack = append(stack, NullValue())
				}
			case KindString:
				if idx.Kind != KindNumber {
					return Value{}, fmt.Errorf("string index must be a number, got %s", idx.typeName())
				}
				k := int(idx.Number)
				if k < 0 || k >= len(target.String) {
					stack = append(stack, NullValue())
				} else {
					stack = append(stack, StringValue(string(target.String[k])))
				}
			case KindNull:
				stack = append(stack, NullValue())
			default:
				return Value{}, fmt.Errorf("cannot index %s", target.typeName())
			}
		case OpAndJump:
			top := stack[len(stack)-1]
			if !top.IsTruthy() {
				pc = int(ins.Arg) // leave the falsy value on the stack
			} else {
				stack = stack[:len(stack)-1]
			}
		case OpOrJump:
			top := stack[len(stack)-1]
			if top.IsTruthy() {
				pc = int(ins.Arg) // leave the truthy value
			} else {
				stack = stack[:len(stack)-1]
			}
		case OpNullishJump:
			top := stack[len(stack)-1]
			if top.Kind != KindNull {
				pc = int(ins.Arg) // leave the non-null value
			} else {
				stack = stack[:len(stack)-1]
			}
		case OpLength:
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			switch v.Kind {
			case KindArray:
				stack = append(stack, NumberValue(float64(len(v.Array))))
			case KindString:
				stack = append(stack, NumberValue(float64(len(v.String))))
			case KindObject:
				stack = append(stack, NumberValue(float64(len(v.Object.Keys))))
			case KindNull:
				stack = append(stack, NumberValue(0))
			default:
				return Value{}, fmt.Errorf("cannot take length of %s", v.typeName())
			}
		case OpMakeArray:
			n := int(ins.Arg)
			els := make([]Value, n)
			copy(els, stack[len(stack)-n:])
			stack = stack[:len(stack)-n]
			stack = append(stack, ArrayValue(els))
		case OpMakeObject:
			n := int(ins.Arg)
			om := NewOrderedMap()
			start := len(stack) - n*2
			for k := 0; k < n; k++ {
				key := stack[start+k*2].String
				val := stack[start+k*2+1]
				om.Set(key, val)
			}
			stack = stack[:start]
			stack = append(stack, ObjectValue(om))
		}
	}
	if len(stack) == 0 {
		return NullValue(), nil
	}
	return stack[len(stack)-1], nil
}

// tryBytecode lowers an expression and runs it on the VM, using the
// per-interpreter cache so each AST node only pays the compile cost
// once. errBytecodeFallback signals the caller to use the tree-walker.
func (i *Interpreter) tryBytecode(e parser.Expr, env *Env) (Value, error) {
	c, hit := i.bcCache[e]
	if !hit {
		var ok bool
		c, ok = CompileExpr(e)
		if !ok {
			i.bcCache[e] = nil
			return Value{}, errBytecodeFallback
		}
		i.bcCache[e] = c
	}
	if c == nil {
		return Value{}, errBytecodeFallback
	}
	return c.Run(i, env)
}

// tryBytecodeStmt lowers a statement (and any nested statements) to
// bytecode and runs it. Caches both successes and refusals against the
// AST node so we only attempt compilation once per node.
func (i *Interpreter) tryBytecodeStmt(s parser.Stmt, env *Env) error {
	c, hit := i.bcStmtCache[s]
	if !hit {
		var ok bool
		c, ok = CompileBlock([]parser.Stmt{s})
		if !ok {
			i.bcStmtCache[s] = nil
			return errBytecodeFallback
		}
		i.bcStmtCache[s] = c
	}
	if c == nil {
		return errBytecodeFallback
	}
	_, err := c.Run(i, env)
	return err
}
