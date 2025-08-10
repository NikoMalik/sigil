
#include "textflag.h"

// func Search(xs []uint64, k uint64) int16
TEXT ·SearchUnroll(SB), NOSPLIT, $0-34
	MOVQ xs_base+0(FP), AX
	MOVQ xs_len+8(FP), CX
	MOVQ k+24(FP), DX

	// Save n
	MOVQ CX, BX

	// Initialize idx register to zero.
	XORL BP, BP

loop:
	// Unroll1
	CMPQ (AX)(BP*8), DX
	JAE  Found

	// Unroll2
	CMPQ 16(AX)(BP*8), DX
	JAE  Found2

	// Unroll3
	CMPQ 32(AX)(BP*8), DX
	JAE  Found3

	// Unroll4
	CMPQ 48(AX)(BP*8), DX
	JAE  Found4

	// plus8
	ADDQ $0x08, BP
	CMPQ BP, CX
	JB   loop
	JMP  NotFound

Found2:
	ADDL $0x02, BP
	JMP  Found

Found3:
	ADDL $0x04, BP
	JMP  Found

Found4:
	ADDL $0x06, BP

Found:
	MOVL BP, BX

NotFound:
	MOVL BX, BP
	SHRL $0x1f, BP
	ADDL BX, BP
	SHRL $0x01, BP
	MOVL BP, ret+32(FP)
	RET









// func SearchSimd(xs []uint64, k uint64) int16
TEXT ·SearchSimd(SB), NOSPLIT, $0-34
    MOVQ xs_base+0(FP), AX
    MOVQ xs_len+8(FP), CX
    MOVQ k+24(FP), DX

    XORQ BP, BP 
    MOVQ CX, BX  

    MOVQ CX, SI
    SHRQ $4, SI   // len / 16
    JZ scalar_loop

vector_loop:
    CMPQ (AX)(BP*8), DX
    JAE found
    CMPQ 16(AX)(BP*8), DX
    JAE found2
    CMPQ 32(AX)(BP*8), DX
    JAE found4
    CMPQ 48(AX)(BP*8), DX
    JAE found6
    CMPQ 64(AX)(BP*8), DX
    JAE found8
    CMPQ 80(AX)(BP*8), DX
    JAE found10
    CMPQ 96(AX)(BP*8), DX
    JAE found12
    CMPQ 112(AX)(BP*8), DX
    JAE found14

    ADDQ $16, BP
    DECQ SI
    JNZ vector_loop
    JMP scalar_loop

found2:
    ADDQ $2, BP
    JMP found
found4:
    ADDQ $4, BP
    JMP found
found6:
    ADDQ $6, BP
    JMP found
found8:
    ADDQ $8, BP
    JMP found
found10:
    ADDQ $10, BP
    JMP found
found12:
    ADDQ $12, BP
    JMP found
found14:
    ADDQ $14, BP

found:
    SHRQ $1, BP
    MOVW BP, ret+32(FP)
    RET

scalar_loop:
    CMPQ BP, BX
    JAE not_found
    CMPQ (AX)(BP*8), DX
    JAE found
    ADDQ $2, BP  
    JMP scalar_loop

not_found:
    MOVQ BX, BP
    SHRQ $1, BP
    MOVW BP, ret+32(FP)
    RET
