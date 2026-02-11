package api

import (
	"encoding/json"
	"fmt"

	"github.com/d4nyll/nodoplex/internal/graph"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// parseCreateNodeRequest extracts the node type and properties from a CreateNodeRequest.
func parseCreateNodeRequest(req CreateNodeRequest) (string, map[string]any, error) {
	disc, err := req.Discriminator()
	if err != nil {
		return "", nil, fmt.Errorf("could not determine node type: %w", err)
	}

	props := map[string]any{}

	switch disc {
	case "vision":
		v, err := req.AsCreateVision()
		if err != nil {
			return "", nil, err
		}
		props["title"] = v.Title
		if v.Description != nil {
			props["description"] = *v.Description
		}

	case "goal":
		v, err := req.AsCreateGoal()
		if err != nil {
			return "", nil, err
		}
		props["title"] = v.Title
		if v.Description != nil {
			props["description"] = *v.Description
		}

	case "feature":
		v, err := req.AsCreateFeature()
		if err != nil {
			return "", nil, err
		}
		props["title"] = v.Title
		if v.Description != nil {
			props["description"] = *v.Description
		}
		if v.Priority != nil {
			props["priority"] = *v.Priority
		}

	case "requirement":
		v, err := req.AsCreateRequirement()
		if err != nil {
			return "", nil, err
		}
		props["title"] = v.Title
		if v.Description != nil {
			props["description"] = *v.Description
		}
		if v.AcceptanceCriteria != nil {
			props["acceptanceCriteria"] = *v.AcceptanceCriteria
		}

	case "task":
		v, err := req.AsCreateTask()
		if err != nil {
			return "", nil, err
		}
		props["title"] = v.Title
		if v.Description != nil {
			props["description"] = *v.Description
		}
		props["status"] = string(v.Status)

	case "gitref":
		v, err := req.AsCreateGitRef()
		if err != nil {
			return "", nil, err
		}
		props["repo"] = v.Repo
		props["refType"] = string(v.RefType)
		props["ref"] = v.Ref
		if v.Url != nil {
			props["url"] = *v.Url
		}
		if v.Message != nil {
			props["message"] = *v.Message
		}

	default:
		return "", nil, fmt.Errorf("unknown node type: %s", disc)
	}

	return disc, props, nil
}

// updateRequestToProps converts an UpdateNodeRequest to a properties map.
func updateRequestToProps(req UpdateNodeRequest) map[string]any {
	props := map[string]any{}
	if req.Title != nil {
		props["title"] = *req.Title
	}
	if req.Description != nil {
		props["description"] = *req.Description
	}
	if req.Priority != nil {
		props["priority"] = *req.Priority
	}
	if req.AcceptanceCriteria != nil {
		props["acceptanceCriteria"] = *req.AcceptanceCriteria
	}
	if req.Status != nil {
		props["status"] = string(*req.Status)
	}
	if req.Repo != nil {
		props["repo"] = *req.Repo
	}
	if req.RefType != nil {
		props["refType"] = string(*req.RefType)
	}
	if req.Ref != nil {
		props["ref"] = *req.Ref
	}
	if req.Url != nil {
		props["url"] = *req.Url
	}
	if req.Message != nil {
		props["message"] = *req.Message
	}
	return props
}

// genericNodeToAPI converts a graph.GenericNode to an api.Node union type.
func genericNodeToAPI(n graph.GenericNode) (Node, error) {
	var node Node

	switch n.Type {
	case graph.NodeTypeVision:
		v := Vision{
			Id:          openapi_types.UUID(n.ID),
			Type:        VisionTypeVision,
			Title:       derefStr(n.Title),
			Description: n.Description,
		}
		if err := node.FromVision(v); err != nil {
			return node, err
		}

	case graph.NodeTypeGoal:
		v := Goal{
			Id:          openapi_types.UUID(n.ID),
			Type:        GoalTypeGoal,
			Title:       derefStr(n.Title),
			Description: n.Description,
		}
		if err := node.FromGoal(v); err != nil {
			return node, err
		}

	case graph.NodeTypeFeature:
		v := Feature{
			Id:          openapi_types.UUID(n.ID),
			Type:        FeatureTypeFeature,
			Title:       derefStr(n.Title),
			Description: n.Description,
			Priority:    n.Priority,
		}
		if err := node.FromFeature(v); err != nil {
			return node, err
		}

	case graph.NodeTypeRequirement:
		v := Requirement{
			Id:                 openapi_types.UUID(n.ID),
			Type:               RequirementTypeRequirement,
			Title:              derefStr(n.Title),
			Description:        n.Description,
			AcceptanceCriteria: n.AcceptanceCriteria,
		}
		if err := node.FromRequirement(v); err != nil {
			return node, err
		}

	case graph.NodeTypeTask:
		status := Backlog
		if n.Status != nil {
			status = TaskStatus(*n.Status)
		}
		v := Task{
			Id:          openapi_types.UUID(n.ID),
			Type:        TaskTypeTask,
			Title:       derefStr(n.Title),
			Description: n.Description,
			Status:      status,
		}
		if err := node.FromTask(v); err != nil {
			return node, err
		}

	case graph.NodeTypeGitRef:
		refType := Commit
		if n.RefType != nil {
			refType = GitRefKind(*n.RefType)
		}
		v := GitRef{
			Id:      openapi_types.UUID(n.ID),
			Type:    GitRefTypeGitref,
			Repo:    derefStr(n.Repo),
			RefType: refType,
			Ref:     derefStr(n.Ref),
			Url:     n.URL,
			Message: n.Message,
		}
		if err := node.FromGitRef(v); err != nil {
			return node, err
		}

	default:
		// Fall back to raw JSON
		raw, _ := json.Marshal(n)
		_ = node.UnmarshalJSON(raw)
	}

	return node, nil
}

// genericNodesToAPI converts a slice of GenericNode to a slice of api.Node.
func genericNodesToAPI(nodes []graph.GenericNode) []Node {
	result := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		an, err := genericNodeToAPI(n)
		if err != nil {
			continue
		}
		result = append(result, an)
	}
	return result
}

// subtreeToAPI converts a graph.SubtreeNode to an api.TreeNode.
func subtreeToAPI(st *graph.SubtreeNode) TreeNode {
	apiNode, _ := genericNodeToAPI(st.Node)
	children := make([]TreeNode, 0, len(st.Children))
	for _, c := range st.Children {
		children = append(children, subtreeToAPI(&c))
	}
	return TreeNode{
		Node:     apiNode,
		Children: children,
	}
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
