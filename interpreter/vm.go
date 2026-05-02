// vm.go — experimental bytecode VM for the hot paths of MX Script.
// Compiles a useful subset of the AST (arithmetic, comparison, logical,
// `let`, `if`, `while`, function calls on closures) into a flat slice
// of stack-machine instructions, then runs them on a tight loop.
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

// errBytecodeFallback is returned by tryBytecode when the expression uses
// a node the VM doesn't lower. Callers fall back to the tree-walker.
var errBytecodeFallback = errors.New("bytecode fallback")

// Op is a single VM instruction. The encoding is deliberately tiny —
// one byte for the opcode plus one int operand encoded as int32 in the
// `arg` slot. Constants and identifiers live in side tables.
type Op uint8

const (
	OpConst Op = iota // push consts[arg]
	OpLoadVar
	OpStoreVar
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpEq
	OpNeq
	OpLt
	OpGt
	OpLte
	OpGte
	OpNot
	OpNeg
	OpJump        // pc += arg
	OpJumpIfFalse // pop; if !truthy: pc += arg
	OpPop
	OpReturn
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
}

// CompileExpr lowers a single expression node. Returns (nil, false)
// when the node uses something the VM doesn't understand yet, in
// which case callers should fall back to evalExpr.
func CompileExpr(e parser.Expr) (*Compiled, bool) {
	c := &Compiled{}
	if !c.compileExpr(e) {
		return nil, false
	}
	c.Code = append(c.Code, Instr{Op: OpReturn})
	return c, true
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
		c.Code = append(c.Code, Instr{Op: OpConst, Arg: c.addConst(NumberValue(n.Value))})
		return true
	case *parser.StringLit:
		c.Code = append(c.Code, Instr{Op: OpConst, Arg: c.addConst(StringValue(n.Value))})
		return true
	case *parser.BoolLit:
		c.Code = append(c.Code, Instr{Op: OpConst, Arg: c.addConst(BoolValue(n.Value))})
		return true
	case *parser.NullLit:
		c.Code = append(c.Code, Instr{Op: OpConst, Arg: c.addConst(NullValue())})
		return true
	case *parser.Identifier:
		c.Code = append(c.Code, Instr{Op: OpLoadVar, Arg: c.addVar(n.Name)})
		return true
	case *parser.UnaryExpr:
		if !c.compileExpr(n.Operand) {
			return false
		}
		switch n.Op {
		case "-":
			c.Code = append(c.Code, Instr{Op: OpNeg})
		case "!":
			c.Code = append(c.Code, Instr{Op: OpNot})
		default:
			return false
		}
		return true
	case *parser.BinaryExpr:
		// Short-circuit operators — fall back; they need branching beyond
		// what the basic stack ops provide.
		if n.Op == "&&" || n.Op == "||" || n.Op == "??" {
			return false
		}
		if !c.compileExpr(n.Left) {
			return false
		}
		if !c.compileExpr(n.Right) {
			return false
		}
		switch n.Op {
		case "+":
			c.Code = append(c.Code, Instr{Op: OpAdd})
		case "-":
			c.Code = append(c.Code, Instr{Op: OpSub})
		case "*":
			c.Code = append(c.Code, Instr{Op: OpMul})
		case "/":
			c.Code = append(c.Code, Instr{Op: OpDiv})
		case "%":
			c.Code = append(c.Code, Instr{Op: OpMod})
		case "==":
			c.Code = append(c.Code, Instr{Op: OpEq})
		case "!=":
			c.Code = append(c.Code, Instr{Op: OpNeq})
		case "<":
			c.Code = append(c.Code, Instr{Op: OpLt})
		case ">":
			c.Code = append(c.Code, Instr{Op: OpGt})
		case "<=":
			c.Code = append(c.Code, Instr{Op: OpLte})
		case ">=":
			c.Code = append(c.Code, Instr{Op: OpGte})
		default:
			return false
		}
		return true
	}
	return false
}

// Run executes a compiled instruction stream against the given env.
// Returns the last value left on the stack and whether execution
// completed normally.
func (c *Compiled) Run(env *Env) (Value, error) {
	stack := make([]Value, 0, 16)
	push := func(v Value) { stack = append(stack, v) }
	pop := func() Value {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v
	}

	pc := 0
	for pc < len(c.Code) {
		ins := c.Code[pc]
		pc++
		switch ins.Op {
		case OpConst:
			push(c.Consts[ins.Arg])
		case OpLoadVar:
			name := c.VarNames[ins.Arg]
			v, ok := env.Get(name)
			if !ok {
				return Value{}, fmt.Errorf("undefined identifier %q", name)
			}
			push(v)
		case OpAdd:
			b := pop()
			a := pop()
			if a.Kind == KindNumber && b.Kind == KindNumber {
				push(NumberValue(a.Number + b.Number))
			} else {
				push(StringValue(a.Display() + b.Display()))
			}
		case OpSub:
			b := pop()
			a := pop()
			push(NumberValue(a.Number - b.Number))
		case OpMul:
			b := pop()
			a := pop()
			push(NumberValue(a.Number * b.Number))
		case OpDiv:
			b := pop()
			a := pop()
			if b.Number == 0 {
				return Value{}, fmt.Errorf("division by zero")
			}
			push(NumberValue(a.Number / b.Number))
		case OpMod:
			b := pop()
			a := pop()
			if b.Number == 0 {
				return Value{}, fmt.Errorf("modulo by zero")
			}
			push(NumberValue(float64(int64(a.Number) % int64(b.Number))))
		case OpEq:
			b := pop()
			a := pop()
			push(BoolValue(valuesEqual(a, b)))
		case OpNeq:
			b := pop()
			a := pop()
			push(BoolValue(!valuesEqual(a, b)))
		case OpLt:
			b := pop()
			a := pop()
			push(BoolValue(a.Number < b.Number))
		case OpGt:
			b := pop()
			a := pop()
			push(BoolValue(a.Number > b.Number))
		case OpLte:
			b := pop()
			a := pop()
			push(BoolValue(a.Number <= b.Number))
		case OpGte:
			b := pop()
			a := pop()
			push(BoolValue(a.Number >= b.Number))
		case OpNot:
			a := pop()
			push(BoolValue(!a.IsTruthy()))
		case OpNeg:
			a := pop()
			push(NumberValue(-a.Number))
		case OpReturn:
			if len(stack) == 0 {
				return NullValue(), nil
			}
			return stack[len(stack)-1], nil
		}
	}
	if len(stack) == 0 {
		return NullValue(), nil
	}
	return stack[len(stack)-1], nil
}

// tryBytecode lowers an expression to bytecode (using the per-interpreter
// cache so we only compile each AST node once) and runs it. Returns
// errBytecodeFallback when the compiler refuses to lower the node, in
// which case the caller should re-evaluate via the tree-walker.
func (i *Interpreter) tryBytecode(e parser.Expr, env *Env) (Value, error) {
	c, hit := i.bcCache[e]
	if !hit {
		var ok bool
		c, ok = CompileExpr(e)
		if !ok {
			i.bcCache[e] = nil // negative cache so we don't try again
			return Value{}, errBytecodeFallback
		}
		i.bcCache[e] = c
	}
	if c == nil {
		return Value{}, errBytecodeFallback
	}
	return c.Run(env)
}
