------------------------ MODULE OperationalTransform ------------------------
(***************************************************************************)
(* This module specifies the Operational Transform based syncing protocol  *)
(* used between the server and multiple clients in paguridate. Each        *)
(* individual session should have its own instance of OT as described in   *)
(* this module. In practice, a UUID might be associated with the whole     *)
(* session instance, while another UUID (as the ClientIDs used in this     *)
(* spec) shall be used to identify each client.                            *)
(***************************************************************************)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANT ClientIDs, N
ASSUME N \in Nat \ {0}

VARIABLES Server, Clients, Connections

(***************************************************************************)
(* Types                                                                   *)
(***************************************************************************)
Change == [client : ClientIDs, version : Nat \ {0}, base : Nat]
ClientState == [ack : Nat, last : Nat]
SendMessage == [ack : Nat, changes : Seq(Change)]
ReceiveMessage == [changes : Seq(Change)]

(***************************************************************************)
(* Utilities                                                               *)
(***************************************************************************)
TotalChanges == N * Cardinality(ClientIDs)
ValidChangeSeq(changes) ==
    /\ Len(changes) <= N
    /\ \A i \in 1..Len(changes) :
        changes[i].version <= N
    /\ \A i \in 1..Len(changes) - 1 :
        /\ changes[i].version + 1 = changes[i + 1].version
        /\ changes[i].base < changes[i + 1].base

SubmittedChanges(client) ==
    SelectSeq(Server.changes, LAMBDA change: change.client = client)

RECURSIVE HasChange(_, _)
HasChange(change, changes) ==
    IF Len(changes) = 0
    THEN FALSE
    ELSE IF Head(changes) = change
         THEN TRUE
         ELSE HasChange(change, Tail(changes))

EmptyState == [ack |-> 0, last |-> 0]
EmptySendMessage == [ack |-> 0, changes |-> << >>]
EmptyReceiveMessage == [changes |-> << >>]

RECURSIVE RecConnected(_, _)
RecConnected(c, conns) ==
    IF Len(conns) = 0
    THEN FALSE
    ELSE IF Head(conns)[1] = c
         THEN TRUE
         ELSE RecConnected(c, Tail(conns))
Connected(c) == RecConnected(c, Connections)

Skip(i, seq) ==
  [j \in 1..(Len(seq)-i) |-> seq[j + i]]

ClientMessage(state) == [ack |-> state.state.ack, changes |-> state.local]
ServerMessage(state, changes) == [changes |-> Skip(state.ack, changes)]

RECURSIVE DisconnectClient(_, _)
DisconnectClient(c, conns) ==
    IF Len(conns) = 0
    THEN conns
    ELSE IF Head(conns)[1] = c
         THEN Tail(conns)
         ELSE << Head(conns) >> \o DisconnectClient(c, Tail(conns))
RECURSIVE FetchConnection(_, _)
FetchConnection(c, conns) ==
    IF Head(conns)[1] = c
    THEN Head(conns)
    ELSE FetchConnection(c, Tail(conns))
MakeConnection(c) ==
    LET client == Clients[c]
        ss == Server.clients[c]
    IN << c, ClientMessage(client), ServerMessage(ss, Server.changes) >>
UpdateClientMessage(c, client) ==
    LET ss == Server.clients[c]
    IN << c, ClientMessage(client), ServerMessage(ss, Server.changes) >>
FetchClientMessage(c) == FetchConnection(c, Connections)[2]
UpdateServerMessageFromState(c, state) ==
    LET client == Clients[c]
    IN << c, ClientMessage(client), ServerMessage(state, Server.changes) >>
FetchServerMessage(c) == FetchConnection(c, Connections)[3]
UpdateConnection(c, conn, conns) == Append(DisconnectClient(c, conns), conn)
AppendNewChanges(changes, conns) ==
    [i \in 1..Len(conns) |->
        LET conn == conns[i]
        IN << conn[1], conn[2],
              [conn[3] EXCEPT !.changes = conn[3].changes \o changes] >>]

RECURSIVE MaxVersion(_, _)
MaxVersion(version, changes) ==
    IF Len(changes) = 0
    THEN version
    ELSE LET nextVersion == IF Head(changes).version > version
                            THEN Head(changes).version
                            ELSE version
         IN MaxVersion(nextVersion, Tail(changes))

UpdateClientStateOnServer(clientAck, changes, oldState) ==
    LET last == MaxVersion(oldState.last, changes)
    IN [ack |-> clientAck, last |-> last]

(***************************************************************************)
(* Invariants                                                              *)
(***************************************************************************)
TypeOK == /\ Server \in [changes : Seq(Change),
                         clients : [ClientIDs -> ClientState]]
          /\ Clients \in [ClientIDs -> [local : Seq(Change),
                                        changes : Seq(Change),
                                        state : ClientState]]
          /\ Connections \in Seq(ClientIDs \X SendMessage \X ReceiveMessage)
          /\ Len(Server.changes) <= TotalChanges
          /\ \A i \in 1..Len(Server.changes) : i > Server.changes[i].base
          /\ \A c \in DOMAIN Clients :
                /\ Clients[c].state.ack = Len(Clients[c].changes)
                /\ ValidChangeSeq(SubmittedChanges(c))
                /\ \A i \in 1..Len(Clients[c].changes) :
                    Server.changes[i] = Clients[c].changes[i]
                /\ Clients[c].state.ack <= Len(Server.changes)
                /\ Clients[c].state.last <= N
                /\ Server.clients[c].ack <= Clients[c].state.ack
                /\ Server.clients[c].last <= Clients[c].state.last

(***************************************************************************)
(* Steps                                                                   *)
(***************************************************************************)
vars == << Server, Clients, Connections >>

Init == /\ Server = [changes |-> << >>,
                     clients |-> [c \in ClientIDs |-> EmptyState]]
        /\ Clients = [c \in ClientIDs |-> [local |-> << >>,
                                           changes |-> << >>,
                                           state |-> EmptyState]]
        /\ Connections = << >>

(***************************************************************************)
(* A client connects to the server.                                        *)
(* In a real setup, a client would have 2 ways to connect to a server:     *)
(* 1. init: a fresh connection to the server is created;                   *)
(* 2. resume: an existing client reconnects to a server;                   *)
(* For simplicity, we ignore the differences between them now.             *)
(***************************************************************************)
Connect ==
    \E c \in ClientIDs :
        /\ Connected(c) = FALSE
        /\ Connections' = Append(Connections, MakeConnection(c))
        /\ UNCHANGED << Server, Clients >>

(***************************************************************************)
(* A client disconnects to the server. Note in current design, both the    *)
(* client and the server preserves all the information when a client       *)
(* disconnects, in a production setup, server might choose to purge        *)
(* clients that remain disconnected after a certain period of time.        *)
(***************************************************************************)
Disconnect ==
    \E c \in ClientIDs :
        /\ Connected(c) = TRUE
        /\ Connections' = DisconnectClient(c, Connections)
        /\ UNCHANGED << Server, Clients >>

(***************************************************************************)
(* A client submits a change.                                              *)
(***************************************************************************)
SubmitChange ==
    \E c \in ClientIDs :
        /\ Connected(c) = TRUE
        /\ LET oldClient == Clients[c]
           IN /\ oldClient.state.last < N
              /\ LET version == oldClient.state.last + 1
                     change == [client |-> c, version |-> version,
                                base |-> oldClient.state.ack]
                     changes == Append(oldClient.local, change)
                     subState == [oldClient.state EXCEPT !.last = version]
                     client == [oldClient EXCEPT !.local = changes,
                                                 !.state = subState]
                     message == UpdateClientMessage(c, client)
                 IN /\ Clients' = [Clients EXCEPT ![c] = client]
                    /\ Connections' = UpdateConnection(c, message,
                                                       Connections)
        /\ UNCHANGED << Server >>


(***************************************************************************)
(* Server receives changes from a client, then broadcast the changes to    *)
(* all clients.                                                            *)
(***************************************************************************)
Receive ==
    \E c \in ClientIDs :
        /\ Connected(c) = TRUE
        /\ LET message == FetchClientMessage(c)
               oldState == Server.clients[c]
               gotChanges == SelectSeq(message.changes,
                                       LAMBDA change: change.client = c)
               last == MaxVersion(0, gotChanges)
               changes == SelectSeq(message.changes,
                                    LAMBDA change: change.version > last)
               newChanges == Server.changes \o changes
               newState == UpdateClientStateOnServer(message.ack, changes,
                                                     oldState)
               newClients == [Server.clients EXCEPT ![c] = newState]
           IN /\ Server' = [Server EXCEPT !.clients = newClients,
                                          !.changes = newChanges]
              /\ Connections' = AppendNewChanges(changes, Connections)
        /\ UNCHANGED << Clients >>

(***************************************************************************)
(* A client receives updates from server, then updates its ack message,    *)
(* purges all local changes that are commited to the server                *)
(***************************************************************************)
Update ==
    \E c \in ClientIDs:
        /\ Connected(c) = TRUE
        /\ LET message == FetchServerMessage(c)
               oldClient == Clients[c]
               oldState == oldClient.state
               oldChanges == oldClient.local
               changes == SelectSeq(message.changes,
                    LAMBDA change: HasChange(change, oldChanges) = FALSE)
               ack == MaxVersion(oldState.ack, changes)
               newState == [oldState EXCEPT !.ack = ack]
               newClient == [local |-> oldChanges \o changes,
                             state |-> newState,
                             changes |-> oldClient.changes \o changes]
               newMessage == UpdateClientMessage(c, newClient)
           IN /\ Clients' = [Clients EXCEPT ![c] = newClient]
              /\ Connections' = UpdateConnection(c, newMessage, Connections)
        /\ UNCHANGED << Server >>

Next == Connect \/ Disconnect \/ SubmitChange \/ Receive \/ Update
Spec == Init /\ [][Next]_vars

=============================================================================
