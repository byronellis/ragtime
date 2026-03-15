# Ragtime

I've been using a couple of different agent harnesses and I'm also doing my own harness now and there's one component
I keep coming back to in all of those implementations: some sort of dynamic context injection system that essentially
replaces a static AGENTS.md for the most part. It's not a novel idea by any means, I got the original idea from Yegge's
`beads prime` implementation, though that one was more or less static last I checked and I've been tending towards things
that are much more context sensitive. For example, telling the agent what *type* of thing it should be doing in a multi-agent
orchestration context, that sort of thing.

I was working on the same thing for Pecan, but I decided that this was useful enough to actually break out into its own
thing since people may want to use it with their own agent harnesses and commercial providers. Hell, *I* want to use it 
with my commercial harnesses since I'm using them to develop things like Pecan and other projects. It also gives me a 
chance to do my favorite thing with tools which is use the tool to build the tool.

And so `ragtime` is born. It's intended as a dynamic agent context injection system where you can write Starlark (why not)
rules to control what context gets injected in any of the supported hook locations, starting with Claude and Gemini since 
those are the two I use personally. 

I was also inspired by projects like Claude Mem (https://github.com/thedotmack/claude-mem) and Memento (https://github.com/Agent-on-the-Fly/Memento)
to add some RAG features to this system as well, hence the name `ragtime.` I'm sure people will argue with me about whether
or not its RAG or whatever, but I don't really care... Like claude-mem I think it will be useful to have an indexed version
of my agent sessions irrespective of agent harness and the ability for all agents to search it for useful nuggets later since
I already use multiple harnesses. Beyond that the ability to quickly search and score at context injection time might prove to
be useful and this is a way of exploring those options.

It'd also be a way of doing a primitive agent orchestration system since the other thing I find myself building are ways to
work with tool approval, one of the most deeply annoying things. One thing I've prototyped a few times is a "approved unless 
blocked" mode where the hook captures the tool request and then starts a countdown timer with a notification. That usually
gives me enough time to hop over from whatever I was doing to check on the approval. If it looks cool I just let the timer
elapse and go about my day. If not I whack Esc or whatever and revise. This requires the agent be running in a multiplexer
so I have the hook wired up to detect that. I find it very useful all by itself so I figured I'd add that here as well.


## Status

Minimum viable — the daemon, hook engine, RAG engine, and session manager are functional. Actively developing.

## Documentation

- [Design Document](docs/design.md) — architecture, components, and design decisions

## Building

```bash
go build -o rt ./cmd/ragtime
```

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.
