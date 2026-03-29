package msg

type Builder struct {
	m Message
}

func BuildFrom(intoMsg IntoMessage) *Builder { return &Builder{m: intoMsg.IntoMessage()} }
func buildMsg(role Role) *Builder            { return &Builder{m: Message{Role: role}} }
func (b *Builder) Build() Message            { return b.m }
func (b *Builder) IntoMessage() Message      { return b.Build() }
func (b *Builder) IntoMessages() []Message   { return []Message{b.Build()} }

func (b *Builder) Text(text string) *Builder { return b.Part(Text(text)) }
func (b *Builder) Thought(thought, signature string) *Builder {
	return b.Part(Thinking(thought, signature))
}
func (b *Builder) Part(part IntoPart) *Builder {
	b.m.Parts = append(b.m.Parts, part.IntoPart())
	return b
}
func (b *Builder) Parts(parts ...IntoPart) *Builder {
	allParts := make(Parts, 0)
	for _, part := range parts {
		allParts = append(allParts, part.IntoPart())
	}
	b.m.Parts = append(b.m.Parts, allParts...)
	return b
}

func (b *Builder) ToolCalls(toolCalls ...ToolCall) *Builder {
	for _, toolCall := range toolCalls {
		b.Part(toolCall)
	}
	return b
}

func (b *Builder) Cache(opts ...CacheOpt) *Builder {
	b.m.CacheHint = NewCacheHint(opts...)
	return b
}

func System(text string) *Builder          { return buildMsg(RoleSystem).Text(text) }
func Developer(text string) *Builder       { return buildMsg(RoleDeveloper).Text(text) }
func User(text string) *Builder            { return buildMsg(RoleUser).Text(text) }
func Assistant(parts ...IntoPart) *Builder { return buildMsg(RoleAssistant).Parts(parts...) }

type ToolMsgBuilder struct {
	b *Builder
}

func Tool() *ToolMsgBuilder { return &ToolMsgBuilder{b: buildMsg(RoleTool)} }
func (b *ToolMsgBuilder) Results(src IntoToolResults) *Builder {
	m := buildMsg(RoleTool)
	for _, result := range src.IntoToolResults() {
		m.Part(result)
	}

	return m
}
