package doctest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestLiveSourceIsExplicitSingleAttemptAndInitializationFree(t *testing.T) {
	source := readRepoFile(t, "internal/live/live_test.go")
	requireText(t, source, "internal/live/live_test.go",
		"//go:build live",
		`os.Getenv("PUSHOVER_LIVE_CONFIRM") != "YES"`,
		`os.Getenv("PUSHOVER_LIVE_TOKEN")`,
		`os.Getenv("PUSHOVER_LIVE_USER_KEY")`,
		"domain.ValidateCredentials",
		`"[pushover-cli live test] " + time.Now().UTC().Format(time.RFC3339)`,
		"Priority: domain.PriorityLowest",
		"pushover.NewClient(nil)",
	)

	parsed, err := parser.ParseFile(token.NewFileSet(), "internal/live/live_test.go", source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse live source: %v", err)
	}
	var liveSend *ast.FuncDecl
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "init" {
			t.Fatal("live package must not define init")
		}
		if ok && function.Name.Name == "TestLiveSend" {
			liveSend = function
		}
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			value := specification.(*ast.ValueSpec)
			if containsClientType(value.Type) {
				t.Fatalf("live package has package-level client variable %q", value.Names[0].Name)
			}
			for _, expression := range value.Values {
				if containsClientConstruction(expression) {
					t.Fatalf("live package constructs a package-level client in variable %q", value.Names[0].Name)
				}
			}
		}
	}
	if liveSend == nil {
		t.Fatal("missing TestLiveSend")
	}

	sendCalls := 0
	loops := 0
	ast.Inspect(liveSend.Body, func(node ast.Node) bool {
		switch node.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			loops++
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "Send" {
			sendCalls++
		}
		return true
	})
	if sendCalls != 1 {
		t.Fatalf("TestLiveSend Send calls = %d, want exactly 1", sendCalls)
	}
	if loops != 0 {
		t.Fatalf("TestLiveSend contains %d loop(s); live send must not retry", loops)
	}

	assertLivePayloadContract(t, liveSend)
}

func assertLivePayloadContract(t *testing.T, liveSend *ast.FuncDecl) {
	t.Helper()
	var send *ast.CallExpr
	ast.Inspect(liveSend.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Send" {
			return true
		}
		client, ok := selector.X.(*ast.Ident)
		if !ok || client.Name != "client" {
			return true
		}
		if send != nil {
			t.Fatal("TestLiveSend has more than one client.Send call")
		}
		send = call
		return true
	})
	if send == nil || len(send.Args) != 2 {
		t.Fatalf("client.Send call = %v; want one call with context and payload arguments", send)
	}

	payload, ok := send.Args[1].(*ast.CompositeLit)
	if !ok || !isSelectorType(payload.Type, "domain", "TargetPayload") {
		t.Fatalf("client.Send payload = %T; want domain.TargetPayload composite literal", send.Args[1])
	}
	message, title, priority := payloadFields(payload)
	if message == nil || !isSelectorType(message.Type, "domain", "Message") {
		t.Fatalf("client.Send payload has no domain.Message field")
	}
	titleIdent, ok := title.(*ast.Ident)
	if !ok {
		t.Fatalf("payload Message.Title = %T; want identifier bound to the live title assignment", title)
	}
	if !isSelectorExpr(priority, "domain", "PriorityLowest") {
		t.Fatalf("payload Message.Priority = %T; want domain.PriorityLowest", priority)
	}

	if !hasExactTopLevelTitleAssignment(liveSend.Body, send, titleIdent.Name) {
		t.Fatalf("title identifier %q must have exactly one exact top-level assignment before client.Send", titleIdent.Name)
	}
}

func hasExactTopLevelTitleAssignment(body *ast.BlockStmt, send *ast.CallExpr, titleName string) bool {
	sendStatement := -1
	for index, statement := range body.List {
		if !statementContainsCall(statement, send) {
			continue
		}
		if sendStatement >= 0 {
			return false
		}
		sendStatement = index
	}
	if sendStatement < 0 {
		return false
	}

	assignments := 0
	exactBeforeSend := 0
	for index, statement := range body.List {
		ast.Inspect(statement, func(node ast.Node) bool {
			switch assignment := node.(type) {
			case *ast.AssignStmt:
				for leftIndex, left := range assignment.Lhs {
					ident, ok := left.(*ast.Ident)
					if ok && ident.Name == titleName && leftIndex < len(assignment.Rhs) {
						assignments++
					}
				}
			case *ast.ValueSpec:
				for valueIndex, name := range assignment.Names {
					if name.Name == titleName && valueIndex < len(assignment.Values) {
						assignments++
					}
				}
			}
			return true
		})
		if index >= sendStatement {
			continue
		}
		if expression, ok := directTitleAssignment(statement, titleName); ok && isLiveTitleExpression(expression) {
			exactBeforeSend++
		}
	}
	return assignments == 1 && exactBeforeSend == 1
}

func statementContainsCall(statement ast.Stmt, want *ast.CallExpr) bool {
	found := false
	ast.Inspect(statement, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && call == want {
			found = true
		}
		return !found
	})
	return found
}

func directTitleAssignment(statement ast.Stmt, titleName string) (ast.Expr, bool) {
	switch statement := statement.(type) {
	case *ast.AssignStmt:
		for index, left := range statement.Lhs {
			ident, ok := left.(*ast.Ident)
			if ok && ident.Name == titleName && index < len(statement.Rhs) {
				return statement.Rhs[index], true
			}
		}
	case *ast.DeclStmt:
		declaration, ok := statement.Decl.(*ast.GenDecl)
		if !ok || declaration.Tok != token.VAR {
			return nil, false
		}
		for _, specification := range declaration.Specs {
			value, ok := specification.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range value.Names {
				if name.Name == titleName && index < len(value.Values) {
					return value.Values[index], true
				}
			}
		}
	}
	return nil, false
}

func TestLiveTitleAssignmentMustBeTopLevelBeforeSend(t *testing.T) {
	const exact = `"[pushover-cli live test] " + time.Now().UTC().Format(time.RFC3339)`
	for _, test := range []struct {
		name   string
		source string
		want   bool
	}{
		{
			name: "top-level before send",
			source: `func TestLiveSend() {
				title := ` + exact + `
				_, err = client.Send(nil, nil)
			}`,
			want: true,
		},
		{
			name: "inner block",
			source: `func TestLiveSend() {
				if true {
					title := ` + exact + `
				}
				_, err = client.Send(nil, nil)
			}`,
		},
		{
			name: "after send",
			source: `func TestLiveSend() {
				_, err = client.Send(nil, nil)
				title := ` + exact + `
			}`,
		},
		{
			name: "reassignment decoy",
			source: `func TestLiveSend() {
				title := ` + exact + `
				title = "decoy"
				_, err = client.Send(nil, nil)
			}`,
		},
		{
			name: "package-level decoy",
			source: `var title = ` + exact + `
			func TestLiveSend() {
				_, err = client.Send(nil, nil)
			}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", "package fixture\n"+test.source, 0)
			if err != nil {
				t.Fatalf("parse fixture: %v", err)
			}
			var function *ast.FuncDecl
			for _, declaration := range file.Decls {
				if candidate, ok := declaration.(*ast.FuncDecl); ok && candidate.Name.Name == "TestLiveSend" {
					function = candidate
					break
				}
			}
			if function == nil {
				t.Fatal("fixture has no function")
			}
			var send *ast.CallExpr
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if ok {
					if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Send" {
						send = call
					}
				}
				return true
			})
			if got := hasExactTopLevelTitleAssignment(function.Body, send, "title"); got != test.want {
				t.Fatalf("hasExactTopLevelTitleAssignment = %t, want %t", got, test.want)
			}
		})
	}
}

func payloadFields(payload *ast.CompositeLit) (message *ast.CompositeLit, title ast.Expr, priority ast.Expr) {
	for _, element := range payload.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := field.Key.(*ast.Ident)
		if !ok || key.Name != "Message" {
			continue
		}
		message, _ = field.Value.(*ast.CompositeLit)
		if message == nil {
			return nil, nil, nil
		}
		for _, messageElement := range message.Elts {
			messageField, ok := messageElement.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			messageKey, ok := messageField.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch messageKey.Name {
			case "Title":
				title = messageField.Value
			case "Priority":
				priority = messageField.Value
			}
		}
		return message, title, priority
	}
	return nil, nil, nil
}

func isSelectorType(expression ast.Expr, packageName, typeName string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := selector.X.(*ast.Ident)
	return ok && ident.Name == packageName && selector.Sel.Name == typeName
}

func isSelectorExpr(expression ast.Expr, packageName, selectorName string) bool {
	return isSelectorType(expression, packageName, selectorName)
}

func isLiveTitleExpression(expression ast.Expr) bool {
	addition, ok := expression.(*ast.BinaryExpr)
	if !ok || addition.Op != token.ADD {
		return false
	}
	prefix, ok := addition.X.(*ast.BasicLit)
	if !ok || prefix.Kind != token.STRING {
		return false
	}
	value, err := strconv.Unquote(prefix.Value)
	if err != nil || value != "[pushover-cli live test] " {
		return false
	}

	format, ok := addition.Y.(*ast.CallExpr)
	if !ok || len(format.Args) != 1 {
		return false
	}
	formatSelector, ok := format.Fun.(*ast.SelectorExpr)
	if !ok || formatSelector.Sel.Name != "Format" || !isSelectorExpr(format.Args[0], "time", "RFC3339") {
		return false
	}
	utc, ok := formatSelector.X.(*ast.CallExpr)
	if !ok || len(utc.Args) != 0 {
		return false
	}
	utcSelector, ok := utc.Fun.(*ast.SelectorExpr)
	if !ok || utcSelector.Sel.Name != "UTC" {
		return false
	}
	now, ok := utcSelector.X.(*ast.CallExpr)
	if !ok || len(now.Args) != 0 {
		return false
	}
	return ok && isSelectorExpr(now.Fun, "time", "Now") && len(now.Args) == 0
}

func containsClientConstruction(expression ast.Expr) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return !found
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return !found
		}
		packageName, packageOK := selector.X.(*ast.Ident)
		if packageOK && packageName.Name == "pushover" && (selector.Sel.Name == "NewClient" || selector.Sel.Name == "New") {
			found = true
		}
		return !found
	})
	return found
}

func containsClientType(expression ast.Expr) bool {
	if expression == nil {
		return false
	}
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "Client" {
			found = true
		}
		return !found
	})
	return found
}

func TestLiveChecksConfirmationAndCredentialsBeforeFactory(t *testing.T) {
	source := readRepoFile(t, "internal/live/live_test.go")
	start := strings.Index(source, "func liveClient(")
	if start < 0 {
		t.Fatal("could not find liveClient")
	}
	end := strings.Index(source[start:], "\n}\n\nfunc TestLiveSendRequiresConfirmationBeforeClient")
	if end < 0 {
		t.Fatal("could not isolate liveClient")
	}
	function := source[start : start+end]
	confirmation := strings.Index(function, `os.Getenv("PUSHOVER_LIVE_CONFIRM")`)
	credentials := strings.Index(function, `os.Getenv("PUSHOVER_LIVE_TOKEN")`)
	validation := strings.Index(function, "domain.ValidateCredentials")
	factory := strings.Index(function, "return factory()")
	if confirmation < 0 || credentials < 0 || validation < 0 || factory < 0 || confirmation >= credentials || credentials >= validation || validation >= factory {
		t.Fatalf("live opt-in order is not confirmation -> credentials -> validation -> factory")
	}
	requireText(t, source, "internal/live/live_test.go",
		`{"missing confirmation"`,
		`{"missing token"`,
		`{"missing user key"`,
	)
}
