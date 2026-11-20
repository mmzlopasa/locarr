package jobs

import "encoding/json"

// decode unmarshals job arguments.
func decode(raw json.RawMessage, v any) error { return json.Unmarshal(raw, v) }

// Decode unmarshals a job's arguments into v.
func (j *Job) Decode(v any) error { return decode(j.Args, v) }
