package core

import (
	"testing"
	"time"
)

// Testing ABA should cover all of the following specifications.
//
// 1. If a correct node outputs the value (b), then every good node outputs (b).
// 2. If all good nodes receive input, then every good node outputs a value.
// 3. If any good node outputs value (b), then at least one good node receives (b)
// as input.

func TestFaultyAgreement(t *testing.T) {
	testAgreement(t, []int{49, 48, 48, 48}, 7776, true, 48)
}

// Test ABA with 2 false and 2 true nodes, cause binary agreement is not a
// majority vote it guarantees that all good nodes output a least the output of
// one good node. Hence the output should be true for all the nodes.
func TestAgreement2FalseNodes(t *testing.T) {
	testAgreement(t, []int{49, 48, 48, 49}, 7876, false, 0)
}

func TestAgreement1FalseNode(t *testing.T) {
	testAgreement(t, []int{49, 48, 49, 49}, 7976, true, 49)
}

func TestAgreementGoodNodes(t *testing.T) {
	testAgreement(t, []int{49, 49, 49, 49}, 8076, true, 49)
}

// @expected indicates if there is an expected decided value
// @expectValue indicates the expected value
func testAgreement(t *testing.T, inputs []int, startPort int, expected bool, expectValue int) {
	num_nodes := len(inputs)
	nodes := Setup(num_nodes, 1, startPort, 3)

	for i, node := range nodes {
		if err := node.Aba.inputValue(inputs[i]); err != nil {
			t.Fatal(err)
		}
	}

	time.Sleep(time.Second)

}
