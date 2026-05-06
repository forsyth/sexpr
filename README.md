# sexpr - SPKI/SDSI-compatible S-expression package

S-expressions ("symbolic expressions") provide a way for programs to store and
exchange tree-structured text and binary data. They are fundamental to the LISP
language, but have wider application.

*Sexpr* is a small Go package to work with the variant defined by [Rivest's Internet Draft](https://github.com/forsyth/sexpr/blob/main/lib/sexp)
(4 May 1997), as used for instance by the Simple Public Key Infrastructure (SPKI).
It can convey binary data directly and efficiently, unlike some other schemes
such as XML. It provides a *canonical* form of S-expression, and an *advanced* form for display.
The two forms are closely related and both can be read or written
by this package, including a variant sometimes used for transport on links that
are not 8-bit safe.

# S-expressions

An S-expression is either a sequence of bytes (a byte string),
or a parenthesised list of smaller S-expressions.
Both canonical and advanced forms start with the fundamental rules below, in extended BNF:

	sexpr ::= string | list
	list ::= "(" sexpr* ")"

That gives the recursive structure.

A given S-expression has a unique canonical form for efficient transmission, storage or signing.
It contains no whitespace, and uses byte counts to delimit raw 8-bit binary strings.
The more readable  "advanced" form allows white space and uses convention and quoting to avoid byte counts,
and resembles the S-expressions used by LISP and other systems.
It is typically used when humans will need to work with it directly. Both forms can be read by the same **Read** operation.

# Go representation

*Sexpr* represents S-expressions using four node types: **String**, **Binary**, and **List**
for the basic syntactic elements, and an **Expr** interface type to represent any one of them
(so **List** is a list of **Expr**). **String** and **Binary** both represent byte strings, but
**String** is specifically a UTF-8 **string** for convenient processing.

All four types satisfy the **encoding.TextMarshaler**, **encoding.BinaryMarshaler** and **fmt.Stringer** interfaces.

A **Reader** produces a stream of **Expr** values from a given **io.Reader** stream.

In the simple case that the S-expression is in a single Go string, **Parse** will parse the string
and return the **Expr**.

See the **go doc** for details.
