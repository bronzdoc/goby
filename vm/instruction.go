package vm

import (
	"fmt"
	"github.com/goby-lang/goby/compiler/bytecode"
	"strings"
)

type operation func(t *thread, cf *callFrame, args ...interface{})

type operationType = string

type setType = string

type action struct {
	name      string
	operation operation
}

type instruction struct {
	action     *action
	Params     []interface{}
	Line       int
	sourceLine int
}

type instructionSet struct {
	name         string
	instructions []*instruction
	filename     filename
	argTypes     []int
}

func (is *instructionSet) define(line int, a *action, params ...interface{}) *instruction {
	i := &instruction{action: a, Params: params, Line: line}
	is.instructions = append(is.instructions, i)
	return i
}

var builtInActions = map[operationType]*action{
	bytecode.Pop: {
		name: bytecode.Pop,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			t.stack.pop()
		},
	},
	bytecode.PutObject: {
		name: bytecode.PutObject,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			object := t.vm.initObjectFromGoType(args[0])
			t.stack.push(&Pointer{Target: object})
		},
	},
	bytecode.GetConstant: {
		name: bytecode.GetConstant,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			constName := args[0].(string)
			c := t.vm.lookupConstant(cf, constName)

			if c == nil {
				err := t.vm.initErrorObject(NameError, "uninitialized constant %s", constName)
				t.stack.push(&Pointer{Target: err})
				return
			}

			c.isNamespace = args[1].(string) == "true"

			if t.stack.top() != nil && t.stack.top().isNamespace {
				t.stack.pop()
			}

			t.stack.push(c)
		},
	},
	bytecode.GetLocal: {
		name: bytecode.GetLocal,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			depth := args[0].(int)
			index := args[1].(int)

			p := cf.getLCL(index, depth)

			if p == nil {
				t.stack.push(&Pointer{Target: NULL})
				return
			}

			t.stack.push(p)
		},
	},
	bytecode.GetInstanceVariable: {
		name: bytecode.GetInstanceVariable,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			variableName := args[0].(string)
			v, ok := cf.self.instanceVariableGet(variableName)
			if !ok {
				t.stack.push(&Pointer{Target: NULL})
				return
			}

			p := &Pointer{Target: v}
			t.stack.push(p)
		},
	},
	bytecode.SetInstanceVariable: {
		name: bytecode.SetInstanceVariable,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			variableName := args[0].(string)
			p := t.stack.pop()
			cf.self.instanceVariableSet(variableName, p.Target)

			var obj Object

			switch v := p.Target.(type) {
			case *HashObject:
				obj = v.copy()
			case *ArrayObject:
				obj = v.copy()
			case *ChannelObject:
				obj = v.copy()
			default:
				obj = v
			}

			t.stack.push(&Pointer{Target: obj})
		},
	},
	bytecode.SetLocal: {
		name: bytecode.SetLocal,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			var optioned bool
			p := t.stack.pop()
			depth := args[0].(int)
			index := args[1].(int)

			if len(args) > 2 && args[2].(int) == 1 {
				optioned = true
			}

			if optioned {
				if cf.getLCL(index, depth) == nil {
					cf.insertLCL(index, depth, p.Target)
				}

				return
			}

			cf.insertLCL(index, depth, p.Target)

			var obj Object

			switch v := p.Target.(type) {
			case *HashObject:
				obj = v.copy()
			case *ArrayObject:
				obj = v.copy()
			case *ChannelObject:
				obj = v.copy()
			default:
				obj = v
			}

			t.stack.push(&Pointer{Target: obj})
		},
	},
	bytecode.SetConstant: {
		name: bytecode.SetConstant,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			constName := args[0].(string)
			c := t.vm.lookupConstant(cf, constName)
			v := t.stack.pop()

			if c != nil {
				err := t.vm.initErrorObject(ConstantAlreadyInitializedError, "Constant %s already been initialized. Can't assign value to a constant twice.", constName)
				t.stack.push(&Pointer{Target: err})
				return
			}

			cf.storeConstant(constName, v)
		},
	},
	bytecode.NewRange: {
		name: bytecode.NewRange,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			rangeEnd := t.stack.pop().Target.(*IntegerObject).value
			rangeStart := t.stack.pop().Target.(*IntegerObject).value

			t.stack.push(&Pointer{Target: t.vm.initRangeObject(rangeStart, rangeEnd)})
		},
	},
	bytecode.NewArray: {
		name: bytecode.NewArray,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			argCount := args[0].(int)
			elems := []Object{}

			for i := 0; i < argCount; i++ {
				v := t.stack.pop()
				elems = append([]Object{v.Target}, elems...)
			}

			arr := t.vm.initArrayObject(elems)
			t.stack.push(&Pointer{Target: arr})
		},
	},
	bytecode.ExpandArray: {
		name: bytecode.ExpandArray,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			arrLength := args[0].(int)
			arr, ok := t.stack.pop().Target.(*ArrayObject)

			if !ok {
				t.returnError(TypeError, "Expect stack top's value to be an Array when executing 'expandarray' instruction.")
				return
			}

			elems := []Object{}

			for i := 0; i < arrLength; i++ {
				var elem Object
				if i < len(arr.Elements) {
					elem = arr.Elements[i]
				} else {
					elem = NULL
				}

				elems = append([]Object{elem}, elems...)
			}

			for _, elem := range elems {
				t.stack.push(&Pointer{Target: elem})
			}
		},
	},
	bytecode.NewHash: {
		name: bytecode.NewHash,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			argCount := args[0].(int)
			pairs := map[string]Object{}

			for i := 0; i < argCount/2; i++ {
				v := t.stack.pop()
				k := t.stack.pop()
				pairs[k.Target.(*StringObject).value] = v.Target
			}

			hash := t.vm.initHashObject(pairs)
			t.stack.push(&Pointer{Target: hash})
		},
	},
	bytecode.BranchUnless: {
		name: bytecode.BranchUnless,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			v := t.stack.pop()
			bool, isBool := v.Target.(*BooleanObject)

			if isBool {
				if bool.value {
					return
				}

				line := args[0].(int)
				cf.pc = line
				return
			}

			_, isNull := v.Target.(*NullObject)

			if isNull {
				line := args[0].(int)
				cf.pc = line
				return
			}
		},
	},
	bytecode.BranchIf: {
		name: bytecode.BranchIf,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			v := t.stack.pop()
			bool, isBool := v.Target.(*BooleanObject)

			if isBool {
				if !bool.value {
					return
				}

				line := args[0].(int)
				cf.pc = line
				return
			}
		},
	},
	bytecode.Jump: {
		name: bytecode.Jump,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			cf.pc = args[0].(int)
		},
	},
	bytecode.PutSelf: {
		name: bytecode.PutSelf,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			t.stack.push(&Pointer{Target: cf.self})
		},
	},
	bytecode.PutString: {
		name: bytecode.PutString,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			object := t.vm.initObjectFromGoType(args[0])
			t.stack.push(&Pointer{Target: object})
		},
	},
	bytecode.PutNull: {
		name: bytecode.PutNull,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			t.stack.push(&Pointer{Target: NULL})
		},
	},
	bytecode.DefMethod: {
		name: bytecode.DefMethod,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			argCount := args[0].(int)
			methodName := t.stack.pop().Target.(*StringObject).value
			is, ok := t.getMethodIS(methodName, cf.instructionSet.filename)

			if !ok {
				t.returnError(InternalError, "Can't get method %s's instruction set.", methodName)
				return
			}

			method := &MethodObject{Name: methodName, argc: argCount, instructionSet: is, baseObj: &baseObj{class: t.vm.topLevelClass(methodClass)}}

			v := t.stack.pop().Target
			switch self := v.(type) {
			case *RClass:
				self.Methods.set(methodName, method)
			default:
				self.Class().Methods.set(methodName, method)
			}
		},
	},
	bytecode.DefSingletonMethod: {
		name: bytecode.DefSingletonMethod,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			argCount := args[0].(int)
			methodName := t.stack.pop().Target.(*StringObject).value
			is, _ := t.getMethodIS(methodName, cf.instructionSet.filename)
			method := &MethodObject{Name: methodName, argc: argCount, instructionSet: is, baseObj: &baseObj{class: t.vm.topLevelClass(methodClass)}}

			v := t.stack.pop().Target

			switch v := v.(type) {
			case *RClass:
				v.SingletonClass().Methods.set(methodName, method)
			default:
				singletonClass := t.vm.createRClass(fmt.Sprintf("#<Class:#<%s:%s>>", v.Class().Name, v.id()))
				singletonClass.Methods.set(methodName, method)
				singletonClass.isSingleton = true
				v.SetSingletonClass(singletonClass)
			}
		},
	},
	bytecode.DefClass: {
		name: bytecode.DefClass,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			subject := strings.Split(args[0].(string), ":")
			subjectType, subjectName := subject[0], subject[1]

			classPtr := cf.lookupConstant(subjectName)

			if classPtr == nil {
				class := t.vm.initializeClass(subjectName, subjectType == "module")
				classPtr = cf.storeConstant(class.Name, class)

				if len(args) >= 2 {
					superClassName := args[1].(string)
					superClass := t.vm.lookupConstant(cf, superClassName)
					inheritedClass, ok := superClass.Target.(*RClass)

					if !ok {
						t.returnError(InternalError, "Constant %s is not a class. got=%s", superClassName, string(superClass.Target.Class().ReturnName()))
						return
					}

					if inheritedClass.isModule {
						t.returnError(InternalError, "Module inheritance is not supported: %s", inheritedClass.Name)
						return
					}

					class.inherits(inheritedClass)
				}
			}

			is := t.getClassIS(subjectName, cf.instructionSet.filename)

			t.stack.pop()
			c := newCallFrame(is)
			c.self = classPtr.Target
			t.callFrameStack.push(c)
			t.startFromTopFrame()

			t.stack.push(classPtr)
		},
	},
	bytecode.Send: {
		name: bytecode.Send,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			var method Object

			methodName := args[0].(string)
			argCount := args[1].(int)
			argPr := t.sp - argCount
			receiverPr := argPr - 1
			receiver := t.stack.Data[receiverPr].Target

			method = receiver.findMethod(methodName)

			if method == nil {
				err := t.vm.initErrorObject(UndefinedMethodError, "Undefined Method '%+v' for %+v", methodName, receiver.toString())
				t.stack.set(receiverPr, &Pointer{Target: err})
				t.sp = argPr
				return
			}

			blockFrame := t.retrieveBlock(cf, args)

			switch m := method.(type) {
			case *MethodObject:
				t.evalMethodObject(receiver, m, receiverPr, argCount, blockFrame)
			case *BuiltInMethodObject:
				t.evalBuiltInMethod(receiver, m, receiverPr, argCount, blockFrame)
			case *Error:
				t.returnError(InternalError, m.toString())
			}
		},
	},
	bytecode.InvokeBlock: {
		name: bytecode.InvokeBlock,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			argCount := args[0].(int)
			argPr := t.sp - argCount
			receiverPr := argPr - 1
			receiver := t.stack.Data[receiverPr].Target

			if cf.blockFrame == nil {
				t.returnError(InternalError, "Can't yield without a block")
				return
			}

			blockFrame := cf.blockFrame

			/*
				This is for such condition:

				```ruby
				def foo(x)
				  yield(x + 10)
				end

				def bar(y)
				  foo(y) do |f|
				    yield(f) # <------- here
				  end
				end

				bar(100) do |b|
				  puts(b) #=> 110
				end
				```

				In this case the target frame is not first block frame we meet. It should be `bar`'s block.
				And bar's frame is foo block frame's ep, so our target frame is ep's block frame.
			*/
			if cf.blockFrame.ep == cf.ep {
				blockFrame = cf.blockFrame.ep.blockFrame
			}

			c := newCallFrame(blockFrame.instructionSet)
			c.blockFrame = blockFrame
			c.ep = blockFrame.ep
			c.self = receiver

			for i := 0; i < argCount; i++ {
				c.locals[i] = t.stack.Data[argPr+i]
			}

			t.callFrameStack.push(c)
			t.startFromTopFrame()

			t.stack.set(receiverPr, t.stack.top())
			t.sp = receiverPr + 1
		},
	},
	bytecode.Leave: {
		name: bytecode.Leave,
		operation: func(t *thread, cf *callFrame, args ...interface{}) {
			//fmt.Println(t.callFrameStack.inspect())
			//fmt.Println("Before leave--------------------------------")
			cf = t.callFrameStack.pop()
			cf.pc = len(cf.instructionSet.instructions)
			//fmt.Println(t.callFrameStack.inspect())

			/*
				Remove top frame if it's a block frame

				Block execution frame <- This was popped when executing leave
				---------------------
				Block frame           <- So this frame is useless
				---------------------
				Main frame
			*/
			topFrame := t.callFrameStack.top()
			if topFrame != nil && topFrame.isBlock {
				cf = t.callFrameStack.pop()
				cf.pc = len(cf.instructionSet.instructions)
			}
		},
	},
}

func (vm *VM) initObjectFromGoType(value interface{}) Object {
	switch v := value.(type) {
	case nil:
		return NULL
	case int:
		return vm.initIntegerObject(v)
	case int64:
		return vm.initIntegerObject(int(v))
	case int32:
		return vm.initIntegerObject(int(v))
	case float64:
		return vm.initIntegerObject(int(v))
	case []uint8:
		bytes := []byte{}

		for _, i := range v {
			bytes = append(bytes, byte(i))
		}

		return vm.initStringObject(string(bytes))
	case string:
		switch v {
		case "true":
			return TRUE
		case "false":
			return FALSE
		default:
			return vm.initStringObject(v)
		}
	case bool:
		if v {
			return TRUE
		}

		return FALSE
	case []interface{}:
		var objs []Object

		for _, elem := range v {
			objs = append(objs, vm.initObjectFromGoType(elem))
		}

		return vm.initArrayObject(objs)
	default:
		return vm.initGoObject(value)
	}
}
