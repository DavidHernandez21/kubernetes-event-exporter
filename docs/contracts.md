# Contracts (interfaces) and Sequence

This document contains two Mermaid diagrams describing the main contracts (interfaces) used by the exporter and a sequence diagram showing an event's path from watcher to sink.

## Interfaces-only (class diagram)

```mermaid
classDiagram
%% Interfaces-only contracts
class Sink {
  <<interface>>
  +Send(ctx context.Context, ev *kube.EnhancedEvent) error
  +Close()
}
class ReceiverRegistry {
  <<interface>>
  +SendEvent(receiver string, event *exporter.EnhancedEvent)
  +Register(name string, r Receiver)
  +Close() error
}
class Engine {
  +OnEvent(event *kube.EnhancedEvent)
}
class Route {
  +ProcessEvent(event *kube.EnhancedEvent)
}
class Rule {
  +MatchesEvent(event *kube.EnhancedEvent) bool
}

Sink <|.. sinks : implements
ReceiverRegistry <|.. ChannelBasedReceiverRegistry
Engine --> Route : uses
Route --> Rule : evaluates
Route --> ReceiverRegistry : dispatches to
```

## Event flow (sequence diagram)

```mermaid
sequenceDiagram
participant Watcher as kube.eventWatcher
participant Lookup as objectMetadataProvider
participant Engine as exporter.Engine
participant Route as exporter.Route
participant Registry as exporter.ReceiverRegistry
participant Sink as Sink

Watcher->>Lookup: optional metadata lookup
alt metadata added
Lookup-->>Watcher: enriched event
end
Watcher->>Engine: OnEvent(enhancedEvent)
Engine->>Route: ProcessEvent(event)
Route->>Rule: MatchesEvent(event)?
alt matched
Route->>Registry: SendEvent(receiverName, ev)
Registry->>Sink: Send(ctx, ev)
else not matched / dropped
Route-->>Engine: dropped
end
```
