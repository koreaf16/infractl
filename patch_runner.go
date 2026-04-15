//go:build tools
// +build tools

package main

import (
	"fmt"
	"log"
	"os/exec"
)

func main() {
	// 27B ?⑥튂 紐낅졊??	cmd27 := `kubectl patch deployment vllm-27b -n dbs --patch '{"spec":{"template":{"spec":{"containers":[{"name":"vllm","args":["codgician/Qwen3.5-27B-Claude-4.6-Opus-Reasoning-Distilled-GPTQ-int4","--gpu-memory-utilization","0.85","--max-num-seqs","4","--max-model-len","131072","--kv-cache-dtype","fp8_e4m3","--enable-chunked-prefill","--port","8000","--trust-remote-code"]}]}}}}'`

	// 35B ?⑥튂 紐낅졊??	cmd35 := `kubectl patch deployment vllm-35b -n dbs --patch '{"spec":{"template":{"spec":{"containers":[{"name":"vllm","args":["Qwen/Qwen3.5-35B-A3B-GPTQ-Int4","--gpu-memory-utilization","0.85","--max-num-seqs","4","--max-model-len","131072","--kv-cache-dtype","fp8_e4m3","--enable-chunked-prefill","--port","8000","--trust-remote-code"]}]}}}}'`

	remoteCmd := fmt.Sprintf("%s && %s", cmd27, cmd35)

	fmt.Println("Executing remote patch via SSH (Go-style)...")

	// SSH 紐낅졊 ?ㅽ뻾 (?몄옄瑜??щ씪?댁뒪濡??꾨떖?섏뿬 ???댁뒪耳?댄봽 諛⑹?)
	out, err := exec.Command("ssh", "-p", "2222", "koreaf16@192.168.0.3", remoteCmd).CombinedOutput()
	if err != nil {
		log.Fatalf("Patch failed: %v\nOutput: %s", err, string(out))
	}

	fmt.Printf("Success!\nOutput: %s\n", string(out))
}

