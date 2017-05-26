package skim

import (
	"bytes"
	"fmt"
	"strconv"
)

// Atom defines any value understood to be a member of a skim list, including lists themselves.
type Atom interface {
	// SkimAtom is an empty method -- it exists only to mark a type as an Atom at compile time.
	SkimAtom()
	String() string
}

type goStringer interface {
	GoString() string
}

func fmtgostring(v interface{}) string {
	switch v := v.(type) {
	case nil:
		return "()"
	case goStringer:
		return v.GoString()
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func fmtstring(v interface{}) string {
	switch v := v.(type) {
	case nil:
		return "()"
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

// The set of runtime atoms

type Int int64

func (Int) SkimAtom()        {}
func (i Int) String() string { return strconv.FormatInt(int64(i), 10) }

type Float float64

func (Float) SkimAtom()        {}
func (f Float) String() string { return strconv.FormatFloat(float64(f), 'f', -1, 64) }

type Symbol string

const (
	noQuote    = Symbol("")
	Quote      = Symbol("quote")
	Quasiquote = Symbol("quasiquote")
	Unquote    = Symbol("unquote")
)

func (Symbol) SkimAtom() {}

func (s Symbol) String() string   { return string(s) }
func (s Symbol) GoString() string { return string(s) }

type Cons struct{ Car, Cdr Atom }

func IsNil(a Atom) bool {
	if a == nil {
		return true
	}
	switch a := a.(type) {
	case *Cons:
		return a == nil || (a.Cdr == nil && a.Car == nil)
	default:
		return false
	}
}

func (*Cons) SkimAtom() {}
func (c *Cons) string(gostring bool) string {
	if c == nil {
		return "'()"
	}

	fmtfn := fmtstring
	if gostring {
		fmtfn = fmtgostring
	} else if !gostring {
		quo := "'"
		switch c.Car {
		case Quote:
		case Unquote:
			quo = ","
		case Quasiquote:
			quo = "`"
		default:
			goto list
		}

		if c, ok := c.Cdr.(*Cons); ok {
			if c.Car == nil && c.Cdr == nil {
				return quo + "()"
			}

			switch c.Cdr.(type) {
			case *Cons:
				return quo + fmtstring(c)
			case nil:
				return quo + fmtstring(c.Car)
			}
		}
	}

list:
	var b bytes.Buffer
	ch := byte('(')
	for c := Atom(c); c != nil; {
		b.WriteByte(ch)
		ch = ' '

		cons, ok := c.(*Cons)
		if !ok {
			b.WriteString(". ")
			b.WriteString(fmtfn(c))
			break
		}

		b.WriteString(fmtfn(cons.Car))
		c = cons.Cdr
	}
	b.WriteByte(')')
	return b.String()
}

func (c *Cons) String() string { return c.string(false) }

func printAtom(a Atom, buf bytes.Buffer, lead, prefix string) bytes.Buffer {
	print := func(args ...interface{}) {
		buf.WriteString(prefix + lead + fmt.Sprint(args...) + "\n")
		lead = ""
	}

	switch a := a.(type) {
	case *Cons:
		print("(")
		buf = printAtom(a.Car, buf, "", prefix+"\t")
		buf = printAtom(a.Cdr, buf, ". ", prefix+"\t")
		print(")")
	default:
		print(a)
	}
	return buf
}

func (c *Cons) GoString() string {
	return "(" + fmtgostring(c.Car) + " . " + fmtgostring(c.Cdr) + ")"
}

type String string

func (String) SkimAtom()          {}
func (s String) String() string   { return s.GoString() }
func (s String) GoString() string { return strconv.QuoteToASCII(string(s)) }

type Bool bool

func (Bool) SkimAtom() {}
func (b Bool) String() string {
	if b {
		return "#t"
	}
	return "#f"
}

type Proc func(*Context, *Cons) (Atom, error)

func (Proc) SkimAtom() {}
func (p Proc) String() string {
	if p == nil {
		return "proc#nil"
	}
	return fmt.Sprintf("proc#%p", p)
}

func Pair(a Atom) (lhs, rhs Atom, err error) {
	la, ok := a.(*Cons)
	if !ok {
		return nil, nil, fmt.Errorf("atom %v is not a cons", a)
	}
	ra, ok := la.Cdr.(*Cons)
	if !ok {
		return nil, nil, fmt.Errorf("atom %v is not a cons", la.Cdr)
	}
	if ra.Cdr != nil {
		return nil, nil, fmt.Errorf("atom %v is not a pair", a)
	}
	return la.Car, ra.Car, nil
}

type Visitor func(Atom) (Visitor, error)

// Traverse will recursively visit all cons pairs and left and right elements, in order. Traversal
// ends when a visitor returns a nil visitor for nested elements and all adjacent and upper elements
// are traversed.
func Traverse(a Atom, visitor Visitor) (err error) {
traverseCdr:
	if IsNil(a) {
		return nil
	}

	visitor, err = visitor(a)
	if err != nil {
		return err
	} else if visitor == nil {
		return nil
	}

	cons, _ := a.(*Cons)
	if cons == nil {
		return nil
	}

	if !IsNil(cons.Car) {
		err = Traverse(cons.Car, visitor)
		if err != nil {
			return nil
		}
	}

	a = cons.Cdr
	goto traverseCdr
}

// Walk recursively visits all cons pairs in a singly-linked list, calling fn for the car of each
// cons pair and walking through each cdr it encounters a nil cdr. If a cdr is encountered that is
// neither a cons pair nor nil, Walk returns an error.
func Walk(a Atom, fn func(Atom) error) error {
	for {
		switch cons := a.(type) {
		case nil:
			return nil
		case *Cons:
			if cons.Car == nil && cons.Cdr == nil {
				// nil / sentinel cons
				return nil
			}

			if err := fn(cons.Car); err != nil {
				return err
			}
			a = cons.Cdr
		default:
			return fmt.Errorf("cannot walk %T", a)
		}
	}
}

func List(a Atom) (list []Atom, err error) {
	n := 0
	if err = Walk(a, func(Atom) error { n++; return nil }); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	n, list = 0, make([]Atom, n)
	Walk(a, func(c Atom) error { list[n] = c; n++; return nil })
	return list, nil
}