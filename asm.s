#include "go_asm.h"
#include "textflag.h"

TEXT ·getFrame(SB),NOSPLIT,$0-8
    MOVD R29, ret+0(FP)
    RET

TEXT ·dupMarker(SB),NOSPLIT,$0-8
    ADR marker, R0
    MOVD R0, ret+0(FP)
    RET
marker:
    WORD $0
