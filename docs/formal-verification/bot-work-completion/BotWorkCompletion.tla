------------------------- MODULE BotWorkCompletion -------------------------
EXTENDS Naturals

CONSTANT MaxAttempts

VARIABLES settled, published, originCurrent, outboxDone, attempts, queued

vars == <<settled, published, originCurrent, outboxDone, attempts, queued>>

Init ==
    /\ settled = FALSE
    /\ published = FALSE
    /\ originCurrent = TRUE
    /\ outboxDone = FALSE
    /\ attempts = 0
    /\ queued = 0

Settle ==
    /\ ~settled
    /\ settled' = TRUE
    /\ UNCHANGED <<published, originCurrent, outboxDone, attempts, queued>>

PublishResult ==
    /\ settled /\ ~published
    /\ published' = TRUE
    /\ UNCHANGED <<settled, originCurrent, outboxDone, attempts, queued>>

InvalidateOrigin ==
    /\ originCurrent
    /\ originCurrent' = FALSE
    /\ UNCHANGED <<settled, published, outboxDone, attempts, queued>>

TryAdmission ==
    /\ published /\ originCurrent /\ ~outboxDone /\ attempts < MaxAttempts
    /\ attempts' = attempts + 1
    /\ queued' = IF queued = 0 THEN 1 ELSE queued
    /\ UNCHANGED <<settled, published, originCurrent, outboxDone>>

FinishEffect ==
    /\ published /\ ~outboxDone /\ (queued > 0 \/ ~originCurrent)
    /\ outboxDone' = TRUE
    /\ UNCHANGED <<settled, published, originCurrent, attempts, queued>>

Next == Settle \/ PublishResult \/ InvalidateOrigin \/ TryAdmission \/ FinishEffect
Spec == Init /\ [][Next]_vars

AtMostOneContinuation == queued <= 1

=============================================================================
