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

CONSTANT ClientIDs, N, ServerID
ASSUME N \in Nat \ {0}
ASSUME ServerID \notin ClientIDs

(***************************************************************************)
(* Utilities                                                               *)
(***************************************************************************)
EmptyState == [ack |-> 0, last |-> 0]
RECURSIVE HasChange(_, _)
HasChange(change, changes) ==
    IF Len(changes) = 0
    THEN FALSE
    ELSE IF Head(changes) = change
         THEN TRUE
         ELSE HasChange(change, Tail(changes))
UnknownChanges(changes, ownedChanges) ==
    SelectSeq(changes, LAMBDA change: \neg HasChange(change, ownedChanges))
RECURSIVE Connected(_, _)
Connected(c, conns) ==
    IF Len(conns) = 0
    THEN FALSE
    ELSE IF Head(conns)[1] = c
         THEN TRUE
         ELSE Connected(c, Tail(conns))
GenMessage(aClientId, aClient, aServer) ==
    << aClientId,
       [ack |-> aClient.state.ack, change |-> aClient.local],
       [changes |-> aServer.changes] >>
EmptyChange == [client |-> (CHOOSE c \in ClientIDs : TRUE),
                version |-> 0,
                base |-> 0]

(***************************************************************************)
(* PlusCal code                                                            *)
(***************************************************************************)
(* --algorithm ProcessChanges {
    variable Server = [changes |-> << >>,
                       acks |-> [c \in ClientIDs |-> 0]],
             Clients = [c \in ClientIDs |-> [local |-> EmptyChange,
                                             changes |-> << >>,
                                             state |-> EmptyState]],
             Connections = << >>;
    fair process (clientId \in ClientIDs)
    variable client, change, changes, message, state;
    {
        ClientLoop:
        while (TRUE) {
            client := Clients[self];
            if (Connected(self, Connections)) {
                either {
                    if (client.state.last < N /\ client.local = EmptyChange) {
                        SubmitChange:
                        change := [client |-> self,
                                   version |-> client.state.last + 1,
                                   base |-> client.state.ack];
                        client.local := change;
                        Clients[self] := client;
                        Connections := Append(
                            SelectSeq(Connections,
                                      LAMBDA aConn : aConn[1] # self),
                            GenMessage(self, client, Server));
                    }
                } or {
                    Update:
                    message := SelectSeq(Connections,
                                      LAMBDA aConn : aConn[1] = self)[1][3];
                    changes := client.changes \o
                        UnknownChanges(message.changes, client.changes);
                    \* Last change submitted by current client or empty
                    change := LET cChanges == SelectSeq(changes,
                                      LAMBDA aChange: aChange.client = self)
                              IN IF Len(cChanges) > 0
                              THEN cChanges[Len(cChanges)]
                              ELSE EmptyChange;
                    \* When a change from current client is acked, use
                    \* it to update last.
                    state := [ack |-> Len(changes),
                              last |-> IF change = EmptyChange
                                       THEN client.state.last
                                       ELSE change.version];
                    client := [local |-> change,
                               changes |-> changes,
                               state |-> state];
                    Clients[self] := client;
                    Connections := [j \in 1..Len(Connections) |->
                        IF Connections[j][1] = self
                        THEN GenMessage(self, client, Server)
                        ELSE Connections[j]];
                } or {
                    Disconnect:
                    Connections := SelectSeq(Connections,
                        LAMBDA aConn : aConn[1] # self);
                };
            } else {
                Connect:
                Connections := Append(Connections,
                    GenMessage(self, client, Server));
            }
        }
    }
    fair process (server = ServerID)
    variable conn, changes;
    {
        ServerLoop:
        while (TRUE) {
            Receive:
            with (i \in 1..Len(Connections)) {
                conn := Connections[i];
                changes := IF conn[2].change = EmptyChange
                           THEN << >>
                           ELSE << conn[2].change >>;
                Server :=
                    [changes |->
                        Server.changes \o UnknownChanges(
                            changes, Server.changes),
                     acks |-> [Server.acks EXCEPT ![conn[1]] = conn[2].ack]];
                Connections := [j \in 1..Len(Connections) |->
                    << Connections[j][1], Connections[j][2],
                       [changes |-> Server.changes] >>];
            }
        }
    }
} *)
\* BEGIN TRANSLATION - the hash of the PCal code: PCal-fa731b0fd2a34fe38792256d6788e2c0
\* Process variable changes of process clientId at line 55 col 30 changed to changes_
CONSTANT defaultInitValue
VARIABLES Server, Clients, Connections, pc, client, change, changes_, message, 
          state, conn, changes

vars == << Server, Clients, Connections, pc, client, change, changes_, 
           message, state, conn, changes >>

ProcSet == (ClientIDs) \cup {ServerID}

Init == (* Global variables *)
        /\ Server = [changes |-> << >>,
                     acks |-> [c \in ClientIDs |-> 0]]
        /\ Clients = [c \in ClientIDs |-> [local |-> EmptyChange,
                                           changes |-> << >>,
                                           state |-> EmptyState]]
        /\ Connections = << >>
        (* Process clientId *)
        /\ client = [self \in ClientIDs |-> defaultInitValue]
        /\ change = [self \in ClientIDs |-> defaultInitValue]
        /\ changes_ = [self \in ClientIDs |-> defaultInitValue]
        /\ message = [self \in ClientIDs |-> defaultInitValue]
        /\ state = [self \in ClientIDs |-> defaultInitValue]
        (* Process server *)
        /\ conn = defaultInitValue
        /\ changes = defaultInitValue
        /\ pc = [self \in ProcSet |-> CASE self \in ClientIDs -> "ClientLoop"
                                        [] self = ServerID -> "ServerLoop"]

ClientLoop(self) == /\ pc[self] = "ClientLoop"
                    /\ client' = [client EXCEPT ![self] = Clients[self]]
                    /\ IF Connected(self, Connections)
                          THEN /\ \/ /\ IF client'[self].state.last < N /\ client'[self].local = EmptyChange
                                           THEN /\ pc' = [pc EXCEPT ![self] = "SubmitChange"]
                                           ELSE /\ pc' = [pc EXCEPT ![self] = "ClientLoop"]
                                  \/ /\ pc' = [pc EXCEPT ![self] = "Update"]
                                  \/ /\ pc' = [pc EXCEPT ![self] = "Disconnect"]
                          ELSE /\ pc' = [pc EXCEPT ![self] = "Connect"]
                    /\ UNCHANGED << Server, Clients, Connections, change, 
                                    changes_, message, state, conn, changes >>

Connect(self) == /\ pc[self] = "Connect"
                 /\ Connections' =            Append(Connections,
                                   GenMessage(self, client[self], Server))
                 /\ pc' = [pc EXCEPT ![self] = "ClientLoop"]
                 /\ UNCHANGED << Server, Clients, client, change, changes_, 
                                 message, state, conn, changes >>

SubmitChange(self) == /\ pc[self] = "SubmitChange"
                      /\ change' = [change EXCEPT ![self] = [client |-> self,
                                                             version |-> client[self].state.last + 1,
                                                             base |-> client[self].state.ack]]
                      /\ client' = [client EXCEPT ![self].local = change'[self]]
                      /\ Clients' = [Clients EXCEPT ![self] = client'[self]]
                      /\ Connections' =            Append(
                                        SelectSeq(Connections,
                                                  LAMBDA aConn : aConn[1] # self),
                                        GenMessage(self, client'[self], Server))
                      /\ pc' = [pc EXCEPT ![self] = "ClientLoop"]
                      /\ UNCHANGED << Server, changes_, message, state, conn, 
                                      changes >>

Update(self) == /\ pc[self] = "Update"
                /\ message' = [message EXCEPT ![self] = SelectSeq(Connections,
                                                               LAMBDA aConn : aConn[1] = self)[1][3]]
                /\ changes_' = [changes_ EXCEPT ![self] =        client[self].changes \o
                                                          UnknownChanges(message'[self].changes, client[self].changes)]
                /\ change' = [change EXCEPT ![self] = LET cChanges == SelectSeq(changes_'[self],
                                                              LAMBDA aChange: aChange.client = self)
                                                      IN IF Len(cChanges) > 0
                                                      THEN cChanges[Len(cChanges)]
                                                      ELSE EmptyChange]
                /\ state' = [state EXCEPT ![self] = [ack |-> Len(changes_'[self]),
                                                     last |-> IF change'[self] = EmptyChange
                                                              THEN client[self].state.last
                                                              ELSE change'[self].version]]
                /\ client' = [client EXCEPT ![self] = [local |-> change'[self],
                                                       changes |-> changes_'[self],
                                                       state |-> state'[self]]]
                /\ Clients' = [Clients EXCEPT ![self] = client'[self]]
                /\ Connections' =            [j \in 1..Len(Connections) |->
                                  IF Connections[j][1] = self
                                  THEN GenMessage(self, client'[self], Server)
                                  ELSE Connections[j]]
                /\ pc' = [pc EXCEPT ![self] = "ClientLoop"]
                /\ UNCHANGED << Server, conn, changes >>

Disconnect(self) == /\ pc[self] = "Disconnect"
                    /\ Connections' =            SelectSeq(Connections,
                                      LAMBDA aConn : aConn[1] # self)
                    /\ pc' = [pc EXCEPT ![self] = "ClientLoop"]
                    /\ UNCHANGED << Server, Clients, client, change, changes_, 
                                    message, state, conn, changes >>

clientId(self) == ClientLoop(self) \/ Connect(self) \/ SubmitChange(self)
                     \/ Update(self) \/ Disconnect(self)

ServerLoop == /\ pc[ServerID] = "ServerLoop"
              /\ pc' = [pc EXCEPT ![ServerID] = "Receive"]
              /\ UNCHANGED << Server, Clients, Connections, client, change, 
                              changes_, message, state, conn, changes >>

Receive == /\ pc[ServerID] = "Receive"
           /\ \E i \in 1..Len(Connections):
                /\ conn' = Connections[i]
                /\ changes' = (IF conn'[2].change = EmptyChange
                               THEN << >>
                               ELSE << conn'[2].change >>)
                /\ Server' = [changes |->
                                 Server.changes \o UnknownChanges(
                                     changes', Server.changes),
                              acks |-> [Server.acks EXCEPT ![conn'[1]] = conn'[2].ack]]
                /\ Connections' =            [j \in 1..Len(Connections) |->
                                  << Connections[j][1], Connections[j][2],
                                     [changes |-> Server'.changes] >>]
           /\ pc' = [pc EXCEPT ![ServerID] = "ServerLoop"]
           /\ UNCHANGED << Clients, client, change, changes_, message, state >>

server == ServerLoop \/ Receive

Next == server
           \/ (\E self \in ClientIDs: clientId(self))

Spec == /\ Init /\ [][Next]_vars
        /\ \A self \in ClientIDs : WF_vars(clientId(self))
        /\ WF_vars(server)

\* END TRANSLATION - the hash of the generated TLA code (remove to silence divergence warnings): TLA-34c925fb4f876f7393ed552f0744b99e

(***************************************************************************)
(* Invariants                                                              *)
(***************************************************************************)
Change == [client : ClientIDs, version : Nat \ {0}, base : Nat]
MaybeChange == Change \union {EmptyChange}
ClientState == [ack : Nat, last : Nat]
SendMessage == [ack : Nat, change : MaybeChange]
ReceiveMessage == [changes : Seq(Change)]

TotalChanges == N * Cardinality(ClientIDs)
ValidChangeSeq(changeSeq) ==
    /\ Len(changeSeq) <= N
    /\ \A i \in 1..Len(changeSeq) :
        changeSeq[i].version <= N
    /\ \A i \in 1..Len(changeSeq) - 1 :
        /\ changeSeq[i].version + 1 = changeSeq[i + 1].version
        \* For a single client, each change should use different base
        /\ changeSeq[i].base < changeSeq[i + 1].base

SubmittedChanges(aClient) ==
    SelectSeq(Server.changes, LAMBDA aChange: aChange.client = aClient)

TypeOK == /\ Server \in [changes : Seq(Change),
                         acks : [ClientIDs -> Nat]]
          /\ Clients \in [ClientIDs -> [local : MaybeChange,
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
                /\ Server.acks[c] <= Clients[c].state.ack

Termination ==
    <> /\ Len(Server.changes) = TotalChanges
       /\ \A c \in ClientIDs : Len(Clients[c].changes) = TotalChanges
=============================================================================
