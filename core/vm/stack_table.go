// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package vm

import (
	"fmt"

	"github.com/ethereum/go-ethereum/params"
)

func pop2push1(stack *Stack) error {
	stackLen := len(stack.data)
	if stackLen < 2 {
		return fmt.Errorf("stack underflow (%d <=> %d)", stackLen, 2)
	}
	if stackLen > int(params.StackLimit)-1 {
		return fmt.Errorf("stack limit reached %d (%d)", stack.len(), params.StackLimit)
	}
	return nil
}
func pop0push1(stack *Stack) error {
	stackLen := len(stack.data)
	if stackLen > int(params.StackLimit)-1 {
		return fmt.Errorf("stack limit reached %d (%d)", stack.len(), params.StackLimit)
	}
	return nil
}
func pop1push0(stack *Stack) error {
	stackLen := len(stack.data)
	if stackLen < 1 {
		return fmt.Errorf("stack underflow (%d <=> %d)", stackLen, 2)
	}
	return nil
}
func pop0push0(stack *Stack) error {
	return nil
}
func makeStackFunc(pop, push int) stackValidationFunc {
	return func(stack *Stack) error {
		stackLen := len(stack.data) - pop
		if stackLen < 0 {
			return fmt.Errorf("stack underflow (%d <=> %d)", stackLen+pop, pop)
		}
		if stackLen+push > int(params.StackLimit) {
			return fmt.Errorf("stack limit reached %d (%d)", stack.len(), params.StackLimit)
		}
		return nil
	}
}

func makeDupStackFunc(n int) stackValidationFunc {
	return makeStackFunc(n, n+1)
}

func makeSwapStackFunc(n int) stackValidationFunc {
	return makeStackFunc(n, n)
}
