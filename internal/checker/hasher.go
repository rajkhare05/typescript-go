package checker

import (
	"slices"
	"strings"

	"github.com/microsoft/typescript-go/internal/ast"
)

type hasher struct {
	sb strings.Builder
}

func (h *hasher) writeString(s string) (int, error) {
	return h.sb.WriteString(s)
}

func (h *hasher) writeByte(b byte) error {
	return h.sb.WriteByte(b)
}

func (h *hasher) String() string {
	return h.sb.String()
}

var base64chars = []byte{
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'A', 'B', 'C', 'D', 'E', 'F',
	'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V',
	'W', 'X', 'Y', 'Z', 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l',
	'm', 'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', '$', '%',
}

func (h *hasher) writeInt(value int) {
	for value != 0 {
		h.writeByte(base64chars[value&0x3F])
		value >>= 6
	}
}

func (h *hasher) writeSymbol(s *ast.Symbol) {
	h.writeInt(int(ast.GetSymbolId(s)))
}

func (h *hasher) writeType(t *Type) {
	h.writeInt(int(t.id))
}

func (h *hasher) writeTypes(types []*Type) {
	i := 0
	var tail bool
	for i < len(types) {
		startId := types[i].id
		count := 1
		for i+count < len(types) && types[i+count].id == startId+TypeId(count) {
			count++
		}
		if tail {
			h.writeByte(',')
		}
		h.writeInt(int(startId))
		if count > 1 {
			h.writeByte(':')
			h.writeInt(count)
		}
		i += count
		tail = true
	}
}

func (h *hasher) writeAlias(alias *TypeAlias) {
	if alias != nil {
		h.writeByte('@')
		h.writeSymbol(alias.symbol)
		if len(alias.typeArguments) != 0 {
			h.writeByte(':')
			h.writeTypes(alias.typeArguments)
		}
	}
}

func (h *hasher) writeGenericTypeReferences(source *Type, target *Type, ignoreConstraints bool) bool {
	var constrained bool
	typeParameters := make([]*Type, 0, 8)
	var writeTypeReference func(*Type, int)
	// writeTypeReference(A<T, number, U>) writes "111=0-12=1"
	// where A.id=111 and number.id=12
	writeTypeReference = func(ref *Type, depth int) {
		h.writeType(ref.Target())
		for _, t := range ref.AsTypeReference().resolvedTypeArguments {
			if t.flags&TypeFlagsTypeParameter != 0 {
				if ignoreConstraints || t.checker.getConstraintOfTypeParameter(t) == nil {
					index := slices.Index(typeParameters, t)
					if index < 0 {
						index = len(typeParameters)
						typeParameters = append(typeParameters, t)
					}
					h.writeByte('=')
					h.writeInt(index)
					continue
				}
				constrained = true
			} else if depth < 4 && isTypeReferenceWithGenericArguments(t) {
				h.writeByte('<')
				writeTypeReference(t, depth+1)
				h.writeByte('>')
				continue
			}
			h.writeByte('-')
			h.writeType(t)
		}
	}
	writeTypeReference(source, 0)
	h.writeByte(',')
	writeTypeReference(target, 0)
	return constrained
}

func (h *hasher) writeNode(node *ast.Node) {
	if node != nil {
		h.writeInt(int(ast.GetNodeId(node)))
	}
}

func getTypeListKey(types []*Type) string {
	var b hasher
	b.writeTypes(types)
	return b.String()
}

func getAliasKey(alias *TypeAlias) string {
	var b hasher
	b.writeAlias(alias)
	return b.String()
}

func getUnionKey(types []*Type, origin *Type, alias *TypeAlias) string {
	var b hasher
	switch {
	case origin == nil:
		b.writeTypes(types)
	case origin.flags&TypeFlagsUnion != 0:
		b.writeByte('|')
		b.writeTypes(origin.Types())
	case origin.flags&TypeFlagsIntersection != 0:
		b.writeByte('&')
		b.writeTypes(origin.Types())
	case origin.flags&TypeFlagsIndex != 0:
		// origin type id alone is insufficient, as `keyof x` may resolve to multiple WIP values while `x` is still resolving
		b.writeByte('#')
		b.writeType(origin)
		b.writeByte('|')
		b.writeTypes(types)
	default:
		panic("Unhandled case in getUnionKey")
	}
	b.writeAlias(alias)
	return b.String()
}

func getIntersectionKey(types []*Type, flags IntersectionFlags, alias *TypeAlias) string {
	var b hasher
	b.writeTypes(types)
	if flags&IntersectionFlagsNoConstraintReduction == 0 {
		b.writeAlias(alias)
	} else {
		b.writeByte('*')
	}
	return b.String()
}

func getTupleKey(elementInfos []TupleElementInfo, readonly bool) string {
	var b hasher
	for _, e := range elementInfos {
		switch {
		case e.flags&ElementFlagsRequired != 0:
			b.writeByte('#')
		case e.flags&ElementFlagsOptional != 0:
			b.writeByte('?')
		case e.flags&ElementFlagsRest != 0:
			b.writeByte('.')
		default:
			b.writeByte('*')
		}
		if e.labeledDeclaration != nil {
			b.writeInt(int(ast.GetNodeId(e.labeledDeclaration)))
		}
	}
	if readonly {
		b.writeByte('!')
	}
	return b.String()
}

func getTypeAliasInstantiationKey(typeArguments []*Type, alias *TypeAlias) string {
	return getTypeInstantiationKey(typeArguments, alias, false)
}

func getTypeInstantiationKey(typeArguments []*Type, alias *TypeAlias, singleSignature bool) string {
	var b hasher
	b.writeTypes(typeArguments)
	b.writeAlias(alias)
	if singleSignature {
		b.writeByte('!')
	}
	return b.String()
}

func getIndexedAccessKey(objectType *Type, indexType *Type, accessFlags AccessFlags, alias *TypeAlias) string {
	var b hasher
	b.writeType(objectType)
	b.writeByte(',')
	b.writeType(indexType)
	b.writeByte(',')
	b.writeInt(int(accessFlags))
	b.writeAlias(alias)
	return b.String()
}

func getTemplateTypeKey(texts []string, types []*Type) string {
	var b hasher
	b.writeTypes(types)
	b.writeByte('|')
	for i, s := range texts {
		if i != 0 {
			b.writeByte(',')
		}
		b.writeInt(len(s))
	}
	b.writeByte('|')
	for _, s := range texts {
		b.writeString(s)
	}
	return b.String()
}

func getConditionalTypeKey(typeArguments []*Type, alias *TypeAlias, forConstraint bool) string {
	var b hasher
	b.writeTypes(typeArguments)
	b.writeAlias(alias)
	if forConstraint {
		b.writeByte('!')
	}
	return b.String()
}

func getRelationKey(source *Type, target *Type, intersectionState IntersectionState, isIdentity bool, ignoreConstraints bool) string {
	if isIdentity && source.id > target.id {
		source, target = target, source
	}
	var b hasher
	var constrained bool
	if isTypeReferenceWithGenericArguments(source) && isTypeReferenceWithGenericArguments(target) {
		constrained = b.writeGenericTypeReferences(source, target, ignoreConstraints)
	} else {
		b.writeType(source)
		b.writeByte(',')
		b.writeType(target)
	}
	if intersectionState != IntersectionStateNone {
		b.writeByte(':')
		b.writeInt(int(intersectionState))
	}
	if constrained {
		// We mark keys with type references that reference constrained type parameters such that we know
		// to obtain and look for a "broadest equivalent key" in the cache.
		b.writeByte('*')
	}
	return b.String()
}

func getNodeListKey(nodes []*ast.Node) string {
	var b hasher
	for i, n := range nodes {
		if i > 0 {
			b.writeByte(',')
		}
		b.writeNode(n)
	}
	return b.String()
}
