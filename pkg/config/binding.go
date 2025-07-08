package config

import (
	"encoding/json"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/tidwall/gjson"
)

type AfterConfigLoaded interface {
	AfterConfigLoaded()
}

type binding struct {
	fieldPath string
	obj       any
}

func (b *binding) Copy(in string) {
	str := in
	if len(b.fieldPath) > 0 {
		res := gjson.Get(in, b.fieldPath)
		if res.Type != gjson.JSON {
			return
		}
		str = res.String()
	}
	data := map[string]interface{}{}
	err := json.Unmarshal([]byte(str), &data)
	if err != nil {
		return
	}

	c := mapstructure.DecoderConfig{
		WeaklyTypedInput: true,
		Result:           b.obj,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToBoolHookFunc(),
			mapstructure.StringToFloat32HookFunc(),
			mapstructure.StringToFloat64HookFunc(),
			mapstructure.StringToIPHookFunc(),
			mapstructure.StringToIPNetHookFunc(),
			mapstructure.OrComposeDecodeHookFunc(
				mapstructure.StringToTimeHookFunc(time.RFC3339),
				mapstructure.StringToTimeHookFunc(time.RFC3339Nano),
			),
		),
	}
	decoder, err := mapstructure.NewDecoder(&c)
	if err != nil {
		return
	}
	err = decoder.Decode(data)
	if err != nil {
		return
	}
	if aclObj, ok := b.obj.(AfterConfigLoaded); ok {
		aclObj.AfterConfigLoaded()
	}
}
