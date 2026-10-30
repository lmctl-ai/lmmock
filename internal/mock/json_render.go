package mock

import (
	"fmt"
	lmcode "github.com/lmctl-ai/lmcode-go"
	"github.com/lmctl-ai/lmmock/pkg/jsontemplate"
)

func registerJSONRender(reg *lmcode.Registry) {
	err := reg.Function("json_render", []string{"Json", "Json"}, "Json", func(a []lmcode.Value) (lmcode.Value, error) {
		template, ok := a[0].Obj.(*lmcode.JSON)
		if !ok {
			return lmcode.None(), fmt.Errorf("json_render: expected Json template")
		}
		context, ok := a[1].Obj.(*lmcode.JSON)
		if !ok {
			return lmcode.None(), fmt.Errorf("json_render: expected Json context")
		}
		text, err := jsontemplate.Render(template.ToJSON(), context.ToJSON())
		if err != nil {
			return lmcode.None(), err
		}
		result, err := lmcode.ParseJSON(text)
		if err != nil {
			return lmcode.None(), err
		}
		return lmcode.Object("Json", result), nil
	})
	if err != nil {
		panic(err)
	}
}
